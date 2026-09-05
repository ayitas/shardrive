package domain

import (
	"errors"
	"testing"
)

func TestErrorPreservesKindAndCause(t *testing.T) {
	cause := errors.New("database detail")
	err := &Error{Kind: ErrConflict, Op: "create", Entity: "chunk", Err: cause}
	if !errors.Is(err, ErrConflict) {
		t.Fatal("errors.Is(error, ErrConflict) = false")
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is(error, cause) = false")
	}
}
