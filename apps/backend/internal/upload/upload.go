package upload

import (
	"fmt"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
)

type State string

const (
	StateCreating  State = "CREATING"
	StateUploading State = "UPLOADING"
	StateVerifying State = "VERIFYING"
	StateCompleted State = "COMPLETED"
	StateExpired   State = "EXPIRED"
	StateCancelled State = "CANCELLED"
	StateFailed    State = "FAILED"
)

func (s State) Valid() bool {
	switch s {
	case StateCreating, StateUploading, StateVerifying, StateCompleted, StateExpired, StateCancelled, StateFailed:
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
	case StateCreating:
		return next == StateUploading || next == StateCancelled || next == StateFailed
	case StateUploading:
		return next == StateVerifying || next == StateExpired || next == StateCancelled || next == StateFailed
	case StateVerifying:
		return next == StateCompleted || next == StateFailed
	default:
		return false
	}
}
func ValidateTransition(from, to State) error {
	if !from.Valid() || !to.Valid() || !from.CanTransitionTo(to) {
		return &domain.Error{Kind: domain.ErrInvalidState, Op: "transition", Entity: "upload", Err: fmt.Errorf("%s to %s", from, to)}
	}
	return nil
}

type Session struct {
	ID              string
	UserID          string
	FileID          string
	ExpectedSize    int64
	ReceivedBytes   int64
	ExpectedChunks  int
	CompletedChunks int
	State           State
	ExpiresAt       time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CreateParams struct {
	UserID         string
	FileID         string
	ExpectedSize   int64
	ExpectedChunks int
	ExpiresAt      time.Time
}

type CreateAggregateParams struct {
	UserID      string
	DirectoryID *string
	Name        string
	MIMEType    string
	SizeBytes   int64
	ChunkSize   int64
	ChunkCount  int
	ExpiresAt   time.Time
}

type CompletionSnapshot struct {
	Session          Session
	File             filedomain.File
	AlreadyCompleted bool
}

type CompletionResult struct {
	FileID         string
	State          filedomain.State
	ChecksumSHA256 string
}
