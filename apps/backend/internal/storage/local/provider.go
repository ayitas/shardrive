package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

type Provider struct {
	root    string
	locksMu sync.Mutex
	locks   map[string]*sync.RWMutex
}

func New(root string) (*Provider, error) {
	if root == "" {
		return nil, fmt.Errorf("create local provider: %w: empty root", storage.ErrInvalidRequest)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local provider root: %w", err)
	}
	return &Provider{root: absolute, locks: make(map[string]*sync.RWMutex)}, nil
}

func (p *Provider) Upload(ctx context.Context, storageAccount account.Account, request storage.UploadRequest) (storage.StoredObject, error) {
	accountID, err := validateAccount(storageAccount)
	if err != nil {
		return storage.StoredObject{}, err
	}
	objectID, finalPath, err := p.objectPath(accountID, request.ObjectID)
	if err != nil {
		return storage.StoredObject{}, err
	}
	if request.Body == nil || request.SizeBytes < 0 {
		return storage.StoredObject{}, fmt.Errorf("upload local object: %w: invalid body or size", storage.ErrInvalidRequest)
	}

	lock := p.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.StoredObject{}, err
	}
	if _, err := os.Stat(finalPath); err == nil {
		return storage.StoredObject{}, fmt.Errorf("upload local object %s: %w", objectID, storage.ErrObjectExists)
	} else if !errors.Is(err, os.ErrNotExist) {
		return storage.StoredObject{}, wrapFilesystem("stat upload target", err)
	}
	usage, err := p.usageUnlocked(ctx, storageAccount, accountID)
	if err != nil {
		return storage.StoredObject{}, err
	}
	if request.SizeBytes > usage.FreeBytes {
		return storage.StoredObject{}, fmt.Errorf("upload local object: %w: need %d bytes, have %d", storage.ErrQuotaExceeded, request.SizeBytes, usage.FreeBytes)
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o750); err != nil {
		return storage.StoredObject{}, wrapFilesystem("create object directory", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(finalPath), ".upload-*")
	if err != nil {
		return storage.StoredObject{}, wrapFilesystem("create temporary object", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	written, copyErr := io.Copy(temporary, io.LimitReader(contextReader{ctx: ctx, reader: request.Body}, request.SizeBytes+1))
	if copyErr != nil {
		return storage.StoredObject{}, fmt.Errorf("stream local object: %w", copyErr)
	}
	if written != request.SizeBytes {
		return storage.StoredObject{}, fmt.Errorf("upload local object: %w: declared %d bytes, received %d", storage.ErrInvalidRequest, request.SizeBytes, written)
	}
	if err := temporary.Sync(); err != nil {
		return storage.StoredObject{}, wrapFilesystem("sync local object", err)
	}
	if err := temporary.Close(); err != nil {
		return storage.StoredObject{}, wrapFilesystem("close local object", err)
	}
	if err := os.Link(temporaryPath, finalPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return storage.StoredObject{}, fmt.Errorf("upload local object %s: %w", objectID, storage.ErrObjectExists)
		}
		return storage.StoredObject{}, wrapFilesystem("commit local object", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		_ = os.Remove(finalPath)
		return storage.StoredObject{}, wrapFilesystem("remove temporary object", err)
	}
	return storage.StoredObject{ObjectID: objectID, SizeBytes: written}, nil
}

func (p *Provider) Download(ctx context.Context, storageAccount account.Account, objectID string) (io.ReadCloser, error) {
	accountID, err := validateAccount(storageAccount)
	if err != nil {
		return nil, err
	}
	objectID, path, err := p.objectPath(accountID, objectID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("download local object %s: %w", objectID, storage.ErrObjectNotFound)
	}
	if err != nil {
		return nil, wrapFilesystem("open local object", err)
	}
	return &contextReadCloser{ctx: ctx, file: file}, nil
}

func (p *Provider) Delete(ctx context.Context, storageAccount account.Account, objectID string) error {
	accountID, err := validateAccount(storageAccount)
	if err != nil {
		return err
	}
	_, path, err := p.objectPath(accountID, objectID)
	if err != nil {
		return err
	}
	lock := p.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapFilesystem("delete local object", err)
	}
	return nil
}

func (p *Provider) Stat(ctx context.Context, storageAccount account.Account, requestedID string) (storage.ObjectInfo, error) {
	accountID, err := validateAccount(storageAccount)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	objectID, path, err := p.objectPath(accountID, requestedID)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if err := ctx.Err(); err != nil {
		return storage.ObjectInfo{}, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return storage.ObjectInfo{}, fmt.Errorf("stat local object %s: %w", objectID, storage.ErrObjectNotFound)
	}
	if err != nil {
		return storage.ObjectInfo{}, wrapFilesystem("stat local object", err)
	}
	if !info.Mode().IsRegular() {
		return storage.ObjectInfo{}, fmt.Errorf("stat local object %s: %w", objectID, storage.ErrObjectNotFound)
	}
	return storage.ObjectInfo{ObjectID: objectID, SizeBytes: info.Size(), ModifiedAt: info.ModTime()}, nil
}

func (p *Provider) Usage(ctx context.Context, storageAccount account.Account) (storage.StorageUsage, error) {
	accountID, err := validateAccount(storageAccount)
	if err != nil {
		return storage.StorageUsage{}, err
	}
	lock := p.accountLock(accountID)
	lock.RLock()
	defer lock.RUnlock()
	return p.usageUnlocked(ctx, storageAccount, accountID)
}

func (p *Provider) Health(ctx context.Context, storageAccount account.Account) error {
	accountID, err := validateAccount(storageAccount)
	if err != nil {
		return err
	}
	lock := p.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Join(p.root, accountID, "objects")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return wrapFilesystem("create account directory", err)
	}
	probe, err := os.CreateTemp(directory, ".health-*")
	if err != nil {
		return wrapFilesystem("probe account directory", err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return wrapFilesystem("close health probe", err)
	}
	if err := os.Remove(name); err != nil {
		return wrapFilesystem("remove health probe", err)
	}
	return nil
}

func (p *Provider) usageUnlocked(ctx context.Context, storageAccount account.Account, accountID string) (storage.StorageUsage, error) {
	objectsRoot := filepath.Join(p.root, accountID, "objects")
	var used int64
	err := filepath.WalkDir(objectsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) && path == objectsRoot {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && filepath.Base(path)[0] != '.' {
			used += info.Size()
		}
		return nil
	})
	if err != nil {
		return storage.StorageUsage{}, wrapFilesystem("calculate local usage", err)
	}
	free := storageAccount.TotalBytes - used
	if free < 0 {
		free = 0
	}
	return storage.StorageUsage{TotalBytes: storageAccount.TotalBytes, UsedBytes: used, FreeBytes: free}, nil
}

func (p *Provider) objectPath(accountID, requestedID string) (string, string, error) {
	objectID, err := storage.NormalizeObjectID(requestedID)
	if err != nil {
		return "", "", fmt.Errorf("resolve local object: %w", err)
	}
	return objectID, filepath.Join(p.root, accountID, "objects", objectID[:2], objectID), nil
}

func (p *Provider) accountLock(accountID string) *sync.RWMutex {
	p.locksMu.Lock()
	defer p.locksMu.Unlock()
	lock := p.locks[accountID]
	if lock == nil {
		lock = &sync.RWMutex{}
		p.locks[accountID] = lock
	}
	return lock
}

func validateAccount(value account.Account) (string, error) {
	id, err := storage.NormalizeObjectID(value.ID)
	if err != nil {
		return "", fmt.Errorf("local account: %w", err)
	}
	if value.TotalBytes < 0 {
		return "", fmt.Errorf("local account: %w: negative quota", storage.ErrInvalidRequest)
	}
	return id, nil
}

func wrapFilesystem(operation string, err error) error {
	return fmt.Errorf("%s: %w: %w", operation, storage.ErrUnavailable, err)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

type contextReadCloser struct {
	ctx  context.Context
	file *os.File
}

func (r *contextReadCloser) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.file.Read(buffer)
}
func (r *contextReadCloser) Close() error { return r.file.Close() }

var _ storage.Provider = (*Provider)(nil)
