package account

import "time"

type State string

const (
	StateActive       State = "ACTIVE"
	StateDegraded     State = "DEGRADED"
	StateFull         State = "FULL"
	StateOffline      State = "OFFLINE"
	StateRateLimited  State = "RATE_LIMITED"
	StateAuthFailed   State = "AUTH_FAILED"
	StateDisabled     State = "DISABLED"
	StateAuthRequired State = "AUTH_REQUIRED"
)

func (s State) Valid() bool {
	switch s {
	case StateActive, StateDegraded, StateFull, StateOffline, StateRateLimited,
		StateAuthFailed, StateDisabled, StateAuthRequired:
		return true
	default:
		return false
	}
}

type Account struct {
	ID                 string
	UserID             string
	Name               string
	Provider           string
	State              State
	TotalBytes         int64
	UsedBytes          int64
	FreeBytes          int64
	Priority           int
	MaxUploadWorkers   int
	MaxDownloadWorkers int
	CredentialRef      *string
	RateLimitedUntil   *time.Time
	LastHealthCheck    *time.Time
	LastError          *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreateParams struct {
	UserID             string
	Name               string
	Provider           string
	TotalBytes         int64
	Priority           int
	MaxUploadWorkers   int
	MaxDownloadWorkers int
	CredentialRef      *string
}

type ProvisionLocalParams struct {
	UserID             string
	Count              int
	TotalBytes         int64
	MaxUploadWorkers   int
	MaxDownloadWorkers int
}
