package file

import (
	"fmt"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/domain"
)

type State string

const (
	StateUploading State = "UPLOADING"
	StateVerifying State = "VERIFYING"
	StateAvailable State = "AVAILABLE"
	StateDegraded  State = "DEGRADED"
	StateDeleting  State = "DELETING"
	StateDeleted   State = "DELETED"
	StateFailed    State = "FAILED"
)

func (s State) Valid() bool {
	switch s {
	case StateUploading, StateVerifying, StateAvailable, StateDegraded, StateDeleting, StateDeleted, StateFailed:
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
	case StateUploading:
		return next == StateVerifying || next == StateFailed || next == StateDeleting
	case StateVerifying:
		return next == StateAvailable || next == StateFailed
	case StateAvailable:
		return next == StateDegraded || next == StateDeleting
	case StateDegraded:
		return next == StateAvailable || next == StateDeleting
	case StateDeleting:
		return next == StateDeleted || next == StateFailed
	default:
		return false
	}
}

func ValidateTransition(from, to State) error {
	if !from.Valid() || !to.Valid() || !from.CanTransitionTo(to) {
		return &domain.Error{Kind: domain.ErrInvalidState, Op: "transition", Entity: "file", Err: fmt.Errorf("%s to %s", from, to)}
	}
	return nil
}

type File struct {
	ID             string
	UserID         string
	DirectoryID    *string
	Name           string
	MIMEType       string
	SizeBytes      int64
	ChunkSize      int64
	ChunkCount     int
	ChecksumSHA256 *string
	State          State
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

type CreateParams struct {
	UserID      string
	DirectoryID *string
	Name        string
	MIMEType    string
	SizeBytes   int64
	ChunkSize   int64
	ChunkCount  int
}
