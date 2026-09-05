package directory

import "time"

type Directory struct {
	ID        string
	UserID    string
	ParentID  *string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
