package upload

import "testing"

func TestTerminalUploadStatesCannotTransition(t *testing.T) {
	for _, state := range []State{StateCompleted, StateExpired, StateCancelled, StateFailed} {
		if state.CanTransitionTo(StateUploading) {
			t.Errorf("%s.CanTransitionTo(UPLOADING) = true", state)
		}
	}
}

func TestUploadHappyPathTransitions(t *testing.T) {
	path := []State{StateCreating, StateUploading, StateVerifying, StateCompleted}
	for i := 0; i < len(path)-1; i++ {
		if err := ValidateTransition(path[i], path[i+1]); err != nil {
			t.Fatalf("ValidateTransition(%s, %s): %v", path[i], path[i+1], err)
		}
	}
}
