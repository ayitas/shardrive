package storage

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
)

func TestRegistry(t *testing.T) {
	registry := NewRegistry()
	local := stubProvider{}
	if err := registry.Register("local", local); err != nil {
		t.Fatalf("Register(local): %v", err)
	}
	if err := registry.Register("local", local); !errors.Is(err, ErrProviderExists) {
		t.Fatalf("duplicate Register() error = %v, want ErrProviderExists", err)
	}
	provider, err := registry.Get("local")
	if err != nil || provider == nil {
		t.Fatalf("Get(local) = %v, %v", provider, err)
	}
	if _, err := registry.Get("proton"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("Get(proton) error = %v, want ErrProviderNotFound", err)
	}
	if got := registry.Names(); len(got) != 1 || got[0] != "local" {
		t.Fatalf("Names() = %v, want [local]", got)
	}
}

func TestRegistryRejectsInvalidRegistrations(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("", stubProvider{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty name error = %v, want ErrInvalidRequest", err)
	}
	if err := registry.Register("local", nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil provider error = %v, want ErrInvalidRequest", err)
	}
}

func TestRetryableErrors(t *testing.T) {
	if !IsRetryable(errors.Join(errors.New("download failed"), ErrUnavailable)) {
		t.Fatal("wrapped ErrUnavailable is not retryable")
	}
	for _, err := range []error{ErrQuotaExceeded, ErrObjectNotFound, ErrInvalidRequest} {
		if IsRetryable(err) {
			t.Errorf("IsRetryable(%v) = true", err)
		}
	}
}

type stubProvider struct{}

func (stubProvider) Upload(context.Context, account.Account, UploadRequest) (StoredObject, error) {
	return StoredObject{}, nil
}
func (stubProvider) Download(context.Context, account.Account, string) (io.ReadCloser, error) {
	return nil, nil
}
func (stubProvider) Delete(context.Context, account.Account, string) error { return nil }
func (stubProvider) Stat(context.Context, account.Account, string) (ObjectInfo, error) {
	return ObjectInfo{}, nil
}
func (stubProvider) Usage(context.Context, account.Account) (StorageUsage, error) {
	return StorageUsage{}, nil
}
func (stubProvider) Health(context.Context, account.Account) error { return nil }
