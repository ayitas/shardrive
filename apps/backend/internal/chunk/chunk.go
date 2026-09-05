package chunk

import (
	"fmt"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/domain"
)

type State string

const (
	StatePending   State = "PENDING"
	StateUploading State = "UPLOADING"
	StateStored    State = "STORED"
	StateCorrupt   State = "CORRUPT"
	StateDeleting  State = "DELETING"
	StateDeleted   State = "DELETED"
	StateFailed    State = "FAILED"
)

func (s State) Valid() bool {
	switch s {
	case StatePending, StateUploading, StateStored, StateCorrupt, StateDeleting, StateDeleted, StateFailed:
		return true
	default:
		return false
	}
}
func (s State) CanTransitionTo(next State) bool {
	if s == next {
		return true
	}
	switch s {
	case StatePending:
		return next == StateUploading || next == StateFailed
	case StateUploading:
		return next == StateStored || next == StateFailed
	case StateStored:
		return next == StateCorrupt || next == StateDeleting
	case StateCorrupt:
		return next == StateDeleting
	case StateDeleting:
		return next == StateDeleted || next == StateFailed
	case StateFailed:
		return next == StateUploading || next == StateDeleting
	default:
		return false
	}
}
func ValidateTransition(from, to State) error {
	if !from.Valid() || !to.Valid() || !from.CanTransitionTo(to) {
		return &domain.Error{Kind: domain.ErrInvalidState, Op: "transition", Entity: "chunk", Err: fmt.Errorf("%s to %s", from, to)}
	}
	return nil
}

type Chunk struct {
	ID               string
	FileID           string
	Index            int
	SizeBytes        int64
	ChecksumSHA256   *string
	StorageAccountID *string
	RemoteObjectID   *string
	RemotePath       *string
	State            State
	RetryCount       int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type StoreParams struct {
	UploadID         string
	FileID           string
	Index            int
	SizeBytes        int64
	ChecksumSHA256   string
	StorageAccountID string
	RemoteObjectID   string
	RemotePath       *string
}
