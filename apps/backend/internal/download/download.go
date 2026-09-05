package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

var ErrIntegrity = errors.New("download integrity check failed")

type plannedChunk struct {
	chunk    chunk.Chunk
	account  account.Account
	provider storage.Provider
}

type Plan struct {
	File   filedomain.File
	chunks []plannedChunk
}

type Service struct {
	files          *filedomain.Repository
	chunks         *chunk.Repository
	accounts       *account.Repository
	providers      *storage.Registry
	accountLimitsM sync.Mutex
	accountLimits  map[string]chan struct{}
}

func NewService(files *filedomain.Repository, chunks *chunk.Repository, accounts *account.Repository, providers *storage.Registry) (*Service, error) {
	if files == nil || chunks == nil || accounts == nil || providers == nil {
		return nil, fmt.Errorf("create download service: %w", domain.ErrInvalid)
	}
	return &Service{
		files: files, chunks: chunks, accounts: accounts, providers: providers,
		accountLimits: make(map[string]chan struct{}),
	}, nil
}

func (s *Service) Prepare(ctx context.Context, userID, fileID string) (Plan, error) {
	normalizedUserID, err := domain.NormalizeUUID(userID)
	if err != nil {
		return Plan{}, &domain.Error{Kind: domain.ErrInvalid, Op: "download", Entity: "file user", Err: err}
	}
	normalizedFileID, err := domain.NormalizeUUID(fileID)
	if err != nil {
		return Plan{}, &domain.Error{Kind: domain.ErrInvalid, Op: "download", Entity: "file ID", Err: err}
	}
	storedFile, err := s.files.GetForUser(ctx, normalizedFileID, normalizedUserID)
	if err != nil {
		return Plan{}, err
	}
	if storedFile.State != filedomain.StateAvailable || storedFile.ChecksumSHA256 == nil {
		return Plan{}, &domain.Error{Kind: domain.ErrInvalidState, Op: "download", Entity: "file"}
	}
	if !validFileLayout(storedFile) {
		return Plan{}, integrityError("file size, chunk size, and chunk count are inconsistent")
	}
	storedChunks, err := s.chunks.ListByFile(ctx, storedFile.ID)
	if err != nil {
		return Plan{}, err
	}
	storageAccounts, err := s.accounts.ListByUser(ctx, normalizedUserID)
	if err != nil {
		return Plan{}, err
	}
	accountsByID := make(map[string]account.Account, len(storageAccounts))
	for _, storageAccount := range storageAccounts {
		accountsByID[storageAccount.ID] = storageAccount
	}
	if len(storedChunks) != storedFile.ChunkCount {
		return Plan{}, integrityError("file has %d/%d chunk mappings", len(storedChunks), storedFile.ChunkCount)
	}
	plan := Plan{File: storedFile, chunks: make([]plannedChunk, 0, len(storedChunks))}
	var totalBytes int64
	for index, storedChunk := range storedChunks {
		expectedSize, err := sizeAt(storedFile, index)
		if err != nil || storedChunk.Index != index || storedChunk.SizeBytes != expectedSize ||
			storedChunk.State != chunk.StateStored || storedChunk.ChecksumSHA256 == nil ||
			storedChunk.StorageAccountID == nil || storedChunk.RemoteObjectID == nil {
			return Plan{}, integrityError("chunk %d metadata is incomplete or inconsistent", index)
		}
		storageAccount, exists := accountsByID[*storedChunk.StorageAccountID]
		if !exists {
			return Plan{}, integrityError("chunk %d references an unavailable user account", index)
		}
		provider, err := s.providers.Get(storageAccount.Provider)
		if err != nil {
			return Plan{}, err
		}
		if err := s.acquireAccount(ctx, storageAccount); err != nil {
			return Plan{}, err
		}
		objectInfo, statErr := provider.Stat(ctx, storageAccount, *storedChunk.RemoteObjectID)
		s.releaseAccount(storageAccount)
		if statErr != nil {
			if errors.Is(statErr, storage.ErrObjectNotFound) {
				return Plan{}, integrityError("chunk %d object is missing", index)
			}
			return Plan{}, statErr
		}
		if objectInfo.ObjectID != *storedChunk.RemoteObjectID || objectInfo.SizeBytes != expectedSize {
			return Plan{}, integrityError("chunk %d object size or identity differs from metadata", index)
		}
		totalBytes += expectedSize
		plan.chunks = append(plan.chunks, plannedChunk{chunk: storedChunk, account: storageAccount, provider: provider})
	}
	if totalBytes != storedFile.SizeBytes {
		return Plan{}, integrityError("chunk bytes %d do not match file size %d", totalBytes, storedFile.SizeBytes)
	}
	return plan, nil
}

func (s *Service) Stream(ctx context.Context, plan Plan, destination io.Writer) error {
	if destination == nil || plan.File.State != filedomain.StateAvailable || plan.File.ChecksumSHA256 == nil || len(plan.chunks) != plan.File.ChunkCount {
		return fmt.Errorf("stream download: %w", domain.ErrInvalid)
	}
	wholeFileHash := sha256.New()
	for index, planned := range plan.chunks {
		if err := s.acquireAccount(ctx, planned.account); err != nil {
			return err
		}
		reader, err := planned.provider.Download(ctx, planned.account, *planned.chunk.RemoteObjectID)
		if err != nil {
			s.releaseAccount(planned.account)
			if errors.Is(err, storage.ErrObjectNotFound) {
				return integrityError("chunk %d object disappeared", index)
			}
			return err
		}
		if reader == nil {
			s.releaseAccount(planned.account)
			return errors.New("storage provider returned a nil download reader")
		}
		chunkHash := sha256.New()
		var verifiedChunk bytes.Buffer
		bytesRead, readErr := io.Copy(io.MultiWriter(&verifiedChunk, wholeFileHash, chunkHash), io.LimitReader(reader, planned.chunk.SizeBytes))
		var extra [1]byte
		extraBytes, trailingErr := io.ReadFull(reader, extra[:])
		closeErr := reader.Close()
		s.releaseAccount(planned.account)
		if readErr != nil {
			return readErr
		}
		if trailingErr != nil && !errors.Is(trailingErr, io.EOF) {
			return trailingErr
		}
		if closeErr != nil {
			return closeErr
		}
		checksum := hex.EncodeToString(chunkHash.Sum(nil))
		if bytesRead != planned.chunk.SizeBytes || extraBytes != 0 || checksum != *planned.chunk.ChecksumSHA256 {
			return integrityError("chunk %d content does not match metadata", index)
		}
		if _, err := io.Copy(destination, &verifiedChunk); err != nil {
			return err
		}
	}
	checksum := hex.EncodeToString(wholeFileHash.Sum(nil))
	if checksum != *plan.File.ChecksumSHA256 {
		return integrityError("whole-file checksum does not match metadata")
	}
	return nil
}

func (s *Service) acquireAccount(ctx context.Context, storageAccount account.Account) error {
	limit := s.accountLimit(storageAccount)
	select {
	case limit <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) releaseAccount(storageAccount account.Account) {
	<-s.accountLimit(storageAccount)
}

func (s *Service) accountLimit(storageAccount account.Account) chan struct{} {
	s.accountLimitsM.Lock()
	defer s.accountLimitsM.Unlock()
	limit := s.accountLimits[storageAccount.ID]
	if limit == nil {
		workers := storageAccount.MaxDownloadWorkers
		if workers < 1 {
			workers = 1
		}
		limit = make(chan struct{}, workers)
		s.accountLimits[storageAccount.ID] = limit
	}
	return limit
}

func sizeAt(storedFile filedomain.File, index int) (int64, error) {
	if !validFileLayout(storedFile) || index < 0 || index >= storedFile.ChunkCount {
		return 0, domain.ErrInvalid
	}
	if index < storedFile.ChunkCount-1 {
		return storedFile.ChunkSize, nil
	}
	return storedFile.SizeBytes - int64(index)*storedFile.ChunkSize, nil
}

func validFileLayout(storedFile filedomain.File) bool {
	if storedFile.SizeBytes < 0 || storedFile.ChunkSize <= 0 || storedFile.ChunkCount < 0 {
		return false
	}
	count := storedFile.SizeBytes / storedFile.ChunkSize
	if storedFile.SizeBytes%storedFile.ChunkSize != 0 {
		count++
	}
	return count == int64(storedFile.ChunkCount)
}

func integrityError(format string, values ...any) error {
	return fmt.Errorf("%w: %s", ErrIntegrity, fmt.Sprintf(format, values...))
}
