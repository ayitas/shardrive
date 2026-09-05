package job

type Type string

const (
	CleanupUpload Type = "CLEANUP_UPLOAD"
	DeleteFile    Type = "DELETE_FILE"
)

type State string

const (
	Pending   State = "PENDING"
	Running   State = "RUNNING"
	Completed State = "COMPLETED"
	Failed    State = "FAILED"
)

type Job struct {
	ID, UserID, Payload   string
	Type                  Type
	State                 State
	Attempts, MaxAttempts int
}
