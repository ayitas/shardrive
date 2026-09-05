package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrInvalid      = errors.New("invalid")
	ErrInvalidState = errors.New("invalid state transition")
)

// Error adds operation and entity context while preserving a stable error kind
// for errors.Is checks at service and HTTP boundaries.
type Error struct {
	Kind   error
	Op     string
	Entity string
	Err    error
}

func (e *Error) Error() string {
	message := fmt.Sprintf("%s %s: %v", e.Op, e.Entity, e.Kind)
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *Error) Unwrap() []error {
	if e.Err == nil {
		return []error{e.Kind}
	}
	return []error{e.Kind, e.Err}
}
