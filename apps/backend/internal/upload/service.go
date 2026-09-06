package upload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/placement"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

const defaultMIMEType = "application/octet-stream"

type ServiceConfig struct {
	ChunkSize         int64
	SessionLifetime   time.Duration
	Now               func() time.Time
	GlobalUploadLimit int
}

type Service struct {
	uploads         *Repository
	chunks          *chunk.Repository
	accounts        *account.Repository
	providers       *storage.Registry
	placement       *placement.Engine
	config          ServiceConfig
	files           *filedomain.Repository
	globalUploads   chan struct{}
	accountLimitsMu sync.Mutex
	accountLimits   map[string]chan struct{}
}

func NewService(uploads *Repository, chunks *chunk.Repository, accounts *account.Repository, providers *storage.Registry, placementEngine *placement.Engine, config ServiceConfig, fileRepos ...*filedomain.Repository) (*Service, error) {
	if uploads == nil || chunks == nil || accounts == nil || providers == nil || placementEngine == nil ||
		config.ChunkSize <= 0 || config.SessionLifetime <= 0 || config.GlobalUploadLimit <= 0 {
		return nil, fmt.Errorf("create upload service: %w", domain.ErrInvalid)
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	var files *filedomain.Repository
	if len(fileRepos) > 0 {
		files = fileRepos[0]
	}
	return &Service{
		uploads: uploads, chunks: chunks, accounts: accounts, providers: providers,
		placement: placementEngine, config: config, files: files,
		globalUploads: make(chan struct{}, config.GlobalUploadLimit),
		accountLimits: make(map[string]chan struct{}),
	}, nil
}

func (s *Service) Cancel(ctx context.Context, userID, uploadID string) error {
	if s.files == nil {
		return &domain.Error{Kind: domain.ErrInvalid, Op: "cancel", Entity: "upload", Err: errors.New("file repository is not configured")}
	}
	normalized, err := domain.NormalizeUUID(userID)
	if err != nil {
		return &domain.Error{Kind: domain.ErrInvalid, Op: "cancel", Entity: "upload user", Err: err}
	}
	session, err := s.uploads.GetForUser(ctx, uploadID, normalized)
	if err != nil {
		return err
	}
	if session.State != StateCreating && session.State != StateUploading {
		return &domain.Error{Kind: domain.ErrConflict, Op: "cancel", Entity: "upload"}
	}
	if _, err := s.uploads.Transition(ctx, session.ID, session.State, StateCancelled); err != nil {
		return err
	}
	file, err := s.files.GetForUser(ctx, session.FileID, normalized)
	if err != nil {
		return err
	}
	if file.State == filedomain.StateUploading {
		_, err = s.files.Transition(ctx, file.ID, file.State, filedomain.StateDeleting)
		return err
	}
	return nil
}

type CreateRequest struct {
	Name        string
	SizeBytes   int64
	MIMEType    string
	DirectoryID *string
}

type CreatedUpload struct {
	Session Session
	File    filedomain.File
}

type Status struct {
	Session          Session
	CompletedIndexes []int
}

type ChunkResult struct {
	Index            int
	State            chunk.State
	SizeBytes        int64
	ChecksumSHA256   string
	StorageAccountID string
	RemoteObjectID   string
	Created          bool
}

func (s *Service) Complete(ctx context.Context, userID, uploadID string) (CompletionResult, error) {
	normalizedUserID, err := domain.NormalizeUUID(userID)
	if err != nil {
		return CompletionResult{}, &domain.Error{Kind: domain.ErrInvalid, Op: "complete", Entity: "upload user", Err: err}
	}
	normalizedUploadID, err := domain.NormalizeUUID(uploadID)
	if err != nil {
		return CompletionResult{}, &domain.Error{Kind: domain.ErrInvalid, Op: "complete", Entity: "upload ID", Err: err}
	}
	snapshot, err := s.uploads.ValidateCompletion(ctx, normalizedUploadID, normalizedUserID)
	if err != nil {
		return CompletionResult{}, err
	}
	if snapshot.AlreadyCompleted {
		return CompletionResult{
			FileID: snapshot.File.ID, State: snapshot.File.State,
			ChecksumSHA256: *snapshot.File.ChecksumSHA256,
		}, nil
	}
	if !s.config.Now().Before(snapshot.Session.ExpiresAt) {
		return CompletionResult{}, &domain.Error{Kind: domain.ErrInvalidState, Op: "complete", Entity: "expired upload"}
	}

	storedChunks, err := s.chunks.ListByFile(ctx, snapshot.File.ID)
	if err != nil {
		return CompletionResult{}, err
	}
	storageAccounts, err := s.accounts.ListByUser(ctx, normalizedUserID)
	if err != nil {
		return CompletionResult{}, err
	}
	accountsByID := make(map[string]account.Account, len(storageAccounts))
	for _, storageAccount := range storageAccounts {
		accountsByID[storageAccount.ID] = storageAccount
	}
	wholeFileHash := sha256.New()
	for index, storedChunk := range storedChunks {
		expectedSize, sizeErr := SizeAt(snapshot.File.SizeBytes, snapshot.File.ChunkSize, index)
		if sizeErr != nil || storedChunk.Index != index || storedChunk.SizeBytes != expectedSize ||
			storedChunk.State != chunk.StateStored || storedChunk.ChecksumSHA256 == nil ||
			storedChunk.StorageAccountID == nil || storedChunk.RemoteObjectID == nil {
			return CompletionResult{}, s.failIntegrity(ctx, snapshot, storedChunk.ID, errors.New("chunk metadata does not match expected sequence"))
		}
		storageAccount, exists := accountsByID[*storedChunk.StorageAccountID]
		if !exists {
			return CompletionResult{}, s.failIntegrity(ctx, snapshot, storedChunk.ID, errors.New("chunk references an unavailable user storage account"))
		}
		provider, err := s.providers.Get(storageAccount.Provider)
		if err != nil {
			return CompletionResult{}, err
		}
		reader, err := provider.Download(ctx, storageAccount, *storedChunk.RemoteObjectID)
		if err != nil {
			if errors.Is(err, storage.ErrObjectNotFound) {
				return CompletionResult{}, s.failIntegrity(ctx, snapshot, storedChunk.ID, err)
			}
			return CompletionResult{}, err
		}
		if reader == nil {
			return CompletionResult{}, errors.New("storage provider returned a nil download reader")
		}
		chunkHash := sha256.New()
		bytesRead, readErr := io.Copy(io.MultiWriter(wholeFileHash, chunkHash), io.LimitReader(reader, expectedSize))
		var extra [1]byte
		extraBytes, trailingErr := io.ReadFull(reader, extra[:])
		closeErr := reader.Close()
		if readErr != nil {
			return CompletionResult{}, readErr
		}
		if trailingErr != nil && !errors.Is(trailingErr, io.EOF) {
			return CompletionResult{}, trailingErr
		}
		if closeErr != nil {
			return CompletionResult{}, closeErr
		}
		actualChecksum := hex.EncodeToString(chunkHash.Sum(nil))
		if bytesRead != expectedSize || extraBytes != 0 || actualChecksum != *storedChunk.ChecksumSHA256 {
			cause := fmt.Errorf("chunk %d integrity mismatch: bytes %d/%d", index, bytesRead, expectedSize)
			return CompletionResult{}, s.failIntegrity(ctx, snapshot, storedChunk.ID, cause)
		}
	}
	checksum := hex.EncodeToString(wholeFileHash.Sum(nil))
	return s.uploads.FinishCompletion(ctx, normalizedUploadID, normalizedUserID, checksum)
}

func (s *Service) failIntegrity(ctx context.Context, snapshot CompletionSnapshot, chunkID string, cause error) error {
	if err := s.uploads.FailIntegrity(ctx, snapshot.Session.ID, snapshot.Session.UserID, chunkID); err != nil {
		return fmt.Errorf("record upload integrity failure: %w", errors.Join(err, cause))
	}
	return &domain.Error{Kind: domain.ErrConflict, Op: "verify", Entity: "upload integrity", Err: cause}
}

func (s *Service) Create(ctx context.Context, userID string, request CreateRequest) (CreatedUpload, error) {
	request.MIMEType = strings.TrimSpace(request.MIMEType)
	if userID == "" || strings.TrimSpace(request.Name) == "" || len(request.Name) > 1024 ||
		request.SizeBytes < 0 || len(request.MIMEType) > 255 {
		return CreatedUpload{}, &domain.Error{Kind: domain.ErrInvalid, Op: "create", Entity: "upload"}
	}
	normalizedUserID, err := domain.NormalizeUUID(userID)
	if err != nil {
		return CreatedUpload{}, &domain.Error{Kind: domain.ErrInvalid, Op: "create", Entity: "upload user", Err: err}
	}
	userID = normalizedUserID
	if request.DirectoryID != nil {
		normalizedDirectoryID, err := domain.NormalizeUUID(*request.DirectoryID)
		if err != nil {
			return CreatedUpload{}, &domain.Error{Kind: domain.ErrInvalid, Op: "create", Entity: "upload directory", Err: err}
		}
		request.DirectoryID = &normalizedDirectoryID
	}
	if request.MIMEType == "" {
		request.MIMEType = defaultMIMEType
	}
	chunkCount, err := ChunkCount(request.SizeBytes, s.config.ChunkSize)
	if err != nil {
		return CreatedUpload{}, err
	}
	expiresAt := s.config.Now().Add(s.config.SessionLifetime)
	storedFile, session, err := s.uploads.CreateWithFile(ctx, CreateAggregateParams{
		UserID: userID, DirectoryID: request.DirectoryID, Name: request.Name,
		MIMEType: request.MIMEType, SizeBytes: request.SizeBytes,
		ChunkSize: s.config.ChunkSize, ChunkCount: chunkCount, ExpiresAt: expiresAt,
	})
	if err != nil {
		return CreatedUpload{}, err
	}
	return CreatedUpload{Session: session, File: storedFile}, nil
}

func (s *Service) UploadChunk(ctx context.Context, userID, uploadID string, index int, body io.Reader, contentLength int64) (ChunkResult, error) {
	if index < 0 {
		return ChunkResult{}, &domain.Error{Kind: domain.ErrInvalid, Op: "upload", Entity: "chunk"}
	}
	status, err := s.Get(ctx, userID, uploadID)
	if err != nil {
		return ChunkResult{}, err
	}
	if status.Session.State != StateUploading || !s.config.Now().Before(status.Session.ExpiresAt) {
		return ChunkResult{}, &domain.Error{Kind: domain.ErrInvalidState, Op: "upload", Entity: "chunk"}
	}
	expectedSize, err := SizeAt(status.Session.ExpectedSize, s.config.ChunkSize, index)
	if err != nil {
		return ChunkResult{}, err
	}
	existing, err := s.chunks.GetByIndex(ctx, status.Session.FileID, index)
	if err == nil {
		if existing.State != chunk.StateStored || existing.ChecksumSHA256 == nil || existing.StorageAccountID == nil || existing.RemoteObjectID == nil {
			return ChunkResult{}, &domain.Error{Kind: domain.ErrConflict, Op: "upload", Entity: "chunk"}
		}
		return resultFromChunk(existing, false), nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return ChunkResult{}, err
	}
	if body == nil || (contentLength >= 0 && contentLength != expectedSize) {
		return ChunkResult{}, &domain.Error{Kind: domain.ErrInvalid, Op: "upload", Entity: "chunk", Err: fmt.Errorf("content length %d, expected %d", contentLength, expectedSize)}
	}

	select {
	case s.globalUploads <- struct{}{}:
		defer func() { <-s.globalUploads }()
	case <-ctx.Done():
		return ChunkResult{}, ctx.Err()
	}
	storageAccounts, err := s.accounts.ListByUser(ctx, userID)
	if err != nil {
		return ChunkResult{}, err
	}
	candidates := make([]placement.Candidate, 0, len(storageAccounts))
	for _, storageAccount := range storageAccounts {
		candidates = append(candidates, placement.Candidate{
			Account: storageAccount, UploadsInFlight: s.accountLoad(storageAccount),
		})
	}
	previousAccountID := ""
	if index > 0 {
		previous, previousErr := s.chunks.GetByIndex(ctx, status.Session.FileID, index-1)
		if previousErr == nil && previous.StorageAccountID != nil {
			previousAccountID = *previous.StorageAccountID
		}
		if previousErr != nil && !errors.Is(previousErr, domain.ErrNotFound) {
			return ChunkResult{}, previousErr
		}
	}

	excluded := make(map[string]struct{})
	var lastErr error
	for len(excluded) < len(candidates) {
		selection, selectErr := s.placement.Select(placement.Request{
			Candidates: candidates, ChunkSizeBytes: expectedSize,
			PreviousAccountID: previousAccountID, ExcludedAccountIDs: excluded,
		})
		if selectErr != nil {
			if lastErr != nil && errors.Is(selectErr, placement.ErrInsufficientStorage) {
				return ChunkResult{}, lastErr
			}
			return ChunkResult{}, selectErr
		}
		selectedAccount := selection.Candidate.Account
		provider, providerErr := s.providers.Get(selectedAccount.Provider)
		if providerErr != nil {
			excluded[selectedAccount.ID] = struct{}{}
			lastErr = providerErr
			continue
		}
		accountLimit := s.accountLimit(selectedAccount)
		select {
		case accountLimit <- struct{}{}:
		case <-ctx.Done():
			return ChunkResult{}, ctx.Err()
		}
		objectID, objectErr := storage.NewObjectID()
		if objectErr != nil {
			<-accountLimit
			return ChunkResult{}, objectErr
		}
		hasher := sha256.New()
		counter := &countingReader{reader: io.TeeReader(body, hasher)}
		storedObject, uploadErr := provider.Upload(ctx, selectedAccount, storage.UploadRequest{
			ObjectID: objectID, SizeBytes: expectedSize, Body: counter,
		})
		<-accountLimit
		if uploadErr != nil {
			excluded[selectedAccount.ID] = struct{}{}
			lastErr = uploadErr
			canRetry := errors.Is(uploadErr, storage.ErrQuotaExceeded) || storage.IsRetryable(uploadErr)
			if counter.bytesRead != 0 || !canRetry {
				return ChunkResult{}, uploadErr
			}
			continue
		}
		if storedObject.ObjectID != objectID || storedObject.SizeBytes != expectedSize || counter.bytesRead != expectedSize {
			_ = provider.Delete(ctx, selectedAccount, objectID)
			return ChunkResult{}, fmt.Errorf("provider returned inconsistent stored object")
		}
		checksum := hex.EncodeToString(hasher.Sum(nil))
		storedChunk, created, storeErr := s.chunks.Store(ctx, chunk.StoreParams{
			UploadID: status.Session.ID, FileID: status.Session.FileID, Index: index,
			SizeBytes: expectedSize, ChecksumSHA256: checksum,
			StorageAccountID: selectedAccount.ID, RemoteObjectID: objectID,
		})
		if storeErr != nil {
			return ChunkResult{}, storeErr
		}
		if !created && storedChunk.RemoteObjectID != nil && *storedChunk.RemoteObjectID != objectID {
			_ = provider.Delete(ctx, selectedAccount, objectID)
		}
		return resultFromChunk(storedChunk, created), nil
	}
	if lastErr != nil {
		return ChunkResult{}, lastErr
	}
	return ChunkResult{}, placement.ErrInsufficientStorage
}

func resultFromChunk(value chunk.Chunk, created bool) ChunkResult {
	result := ChunkResult{Index: value.Index, State: value.State, SizeBytes: value.SizeBytes, Created: created}
	if value.ChecksumSHA256 != nil {
		result.ChecksumSHA256 = *value.ChecksumSHA256
	}
	if value.StorageAccountID != nil {
		result.StorageAccountID = *value.StorageAccountID
	}
	if value.RemoteObjectID != nil {
		result.RemoteObjectID = *value.RemoteObjectID
	}
	return result
}

func (s *Service) accountLimit(value account.Account) chan struct{} {
	s.accountLimitsMu.Lock()
	defer s.accountLimitsMu.Unlock()
	limit := s.accountLimits[value.ID]
	if limit == nil {
		workers := value.MaxUploadWorkers
		if workers < 1 {
			workers = 1
		}
		limit = make(chan struct{}, workers)
		s.accountLimits[value.ID] = limit
	}
	return limit
}

func (s *Service) accountLoad(value account.Account) int {
	return len(s.accountLimit(value))
}

type countingReader struct {
	reader    io.Reader
	bytesRead int64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	r.bytesRead += int64(count)
	return count, err
}

func (s *Service) Get(ctx context.Context, userID, uploadID string) (Status, error) {
	if userID == "" || uploadID == "" {
		return Status{}, &domain.Error{Kind: domain.ErrInvalid, Op: "get", Entity: "upload"}
	}
	normalizedUserID, err := domain.NormalizeUUID(userID)
	if err != nil {
		return Status{}, &domain.Error{Kind: domain.ErrInvalid, Op: "get", Entity: "upload user", Err: err}
	}
	normalizedUploadID, err := domain.NormalizeUUID(uploadID)
	if err != nil {
		return Status{}, &domain.Error{Kind: domain.ErrInvalid, Op: "get", Entity: "upload ID", Err: err}
	}
	session, err := s.uploads.GetForUser(ctx, normalizedUploadID, normalizedUserID)
	if err != nil {
		return Status{}, err
	}
	chunks, err := s.chunks.ListByFile(ctx, session.FileID)
	if err != nil {
		return Status{}, err
	}
	indexes := make([]int, 0, len(chunks))
	for _, storedChunk := range chunks {
		if storedChunk.State == chunk.StateStored {
			indexes = append(indexes, storedChunk.Index)
		}
	}
	return Status{Session: session, CompletedIndexes: indexes}, nil
}

func (s *Service) ListActive(ctx context.Context, userID string) ([]ResumeStatus, error) {
	normalizedUserID, err := domain.NormalizeUUID(userID)
	if err != nil {
		return nil, &domain.Error{Kind: domain.ErrInvalid, Op: "list", Entity: "upload user", Err: err}
	}
	values, err := s.uploads.ListActiveForUser(ctx, normalizedUserID)
	if err != nil {
		return nil, err
	}
	for index := range values {
		chunks, chunkErr := s.chunks.ListByFile(ctx, values[index].FileID)
		if chunkErr != nil {
			return nil, chunkErr
		}
		values[index].CompletedIndexes = make([]int, 0, len(chunks))
		for _, storedChunk := range chunks {
			if storedChunk.State == chunk.StateStored {
				values[index].CompletedIndexes = append(values[index].CompletedIndexes, storedChunk.Index)
			}
		}
	}
	return values, nil
}

func ChunkCount(sizeBytes, chunkSize int64) (int, error) {
	if sizeBytes < 0 || chunkSize <= 0 {
		return 0, &domain.Error{Kind: domain.ErrInvalid, Op: "calculate", Entity: "chunk count"}
	}
	count := sizeBytes / chunkSize
	if sizeBytes%chunkSize != 0 {
		count++
	}
	if count > math.MaxInt32 {
		return 0, &domain.Error{Kind: domain.ErrInvalid, Op: "calculate", Entity: "chunk count", Err: errors.New("exceeds PostgreSQL integer range")}
	}
	return int(count), nil
}

func SizeAt(sizeBytes, chunkSize int64, index int) (int64, error) {
	count, err := ChunkCount(sizeBytes, chunkSize)
	if err != nil {
		return 0, err
	}
	if index < 0 || index >= count {
		return 0, &domain.Error{Kind: domain.ErrInvalid, Op: "calculate", Entity: "chunk size", Err: fmt.Errorf("index %d outside [0,%d)", index, count)}
	}
	if index < count-1 {
		return chunkSize, nil
	}
	return sizeBytes - int64(index)*chunkSize, nil
}
