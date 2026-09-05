package file

import (
	"errors"
	"testing"

	"github.com/ayitas/shardrive/apps/backend/internal/domain"
)

func TestStateTransitions(t *testing.T) {
	tests := []struct {
		name string
		from State
		to   State
		want bool
	}{
		{"upload verifies", StateUploading, StateVerifying, true},
		{"verification succeeds", StateVerifying, StateAvailable, true},
		{"available deletes", StateAvailable, StateDeleting, true},
		{"deleted is terminal", StateDeleted, StateAvailable, false},
		{"cannot skip verification", StateUploading, StateAvailable, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransition(tt.from, tt.to)
			if (err == nil) != tt.want {
				t.Fatalf("ValidateTransition(%q, %q) error = %v, want valid=%v", tt.from, tt.to, err, tt.want)
			}
			if !tt.want && !errors.Is(err, domain.ErrInvalidState) {
				t.Fatalf("error = %v, want ErrInvalidState", err)
			}
		})
	}
}
