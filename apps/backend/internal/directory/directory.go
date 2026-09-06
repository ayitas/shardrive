package directory

import "time"

type Directory struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	ParentID  *string   `json:"parentId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
