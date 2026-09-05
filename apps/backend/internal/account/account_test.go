package account

import "testing"

func TestAllAccountStatesAreValid(t *testing.T) {
	states := []State{
		StateActive, StateDegraded, StateFull, StateOffline, StateRateLimited,
		StateAuthFailed, StateDisabled, StateAuthRequired,
	}
	for _, state := range states {
		if !state.Valid() {
			t.Errorf("%q.Valid() = false", state)
		}
	}
	if State("UNKNOWN").Valid() {
		t.Fatal("UNKNOWN.Valid() = true")
	}
}
