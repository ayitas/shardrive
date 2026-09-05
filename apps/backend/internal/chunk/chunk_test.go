package chunk

import "testing"

func TestChunkTransitions(t *testing.T) {
	if err := ValidateTransition(StatePending, StateUploading); err != nil {
		t.Fatalf("pending to uploading: %v", err)
	}
	if err := ValidateTransition(StateUploading, StateStored); err != nil {
		t.Fatalf("uploading to stored: %v", err)
	}
	if err := ValidateTransition(StateStored, StatePending); err == nil {
		t.Fatal("stored to pending error = nil")
	}
}
