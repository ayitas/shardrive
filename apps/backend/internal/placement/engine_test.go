package placement

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
)

const gibibyte int64 = 1024 * 1024 * 1024

func TestRequiredReserveUsesLargerDefault(t *testing.T) {
	engine := NewDefault()
	tests := []struct {
		chunk int64
		want  int64
	}{
		{32 * mebibyte, 256 * mebibyte},
		{128 * mebibyte, 512 * mebibyte},
	}
	for _, test := range tests {
		got, ok := engine.RequiredReserve(test.chunk)
		if !ok || got != test.want {
			t.Errorf("RequiredReserve(%d) = %d, %v; want %d, true", test.chunk, got, ok, test.want)
		}
	}
}

func TestFilteringRejectsIneligibleAccounts(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	chunkSize := int64(32 * mebibyte)
	reserve := int64(256 * mebibyte)

	tests := []struct {
		name      string
		candidate Candidate
		excluded  map[string]struct{}
		want      bool
	}{
		{"active", candidate("active", account.StateActive, gibibyte, 0), nil, true},
		{"offline", candidate("offline", account.StateOffline, gibibyte, 0), nil, false},
		{"full state", candidate("full", account.StateFull, gibibyte, 0), nil, false},
		{"disabled", candidate("disabled", account.StateDisabled, gibibyte, 0), nil, false},
		{"degraded", candidate("degraded", account.StateDegraded, gibibyte, 0), nil, false},
		{"auth failed", candidate("auth", account.StateAuthFailed, gibibyte, 0), nil, false},
		{"explicitly excluded", candidate("excluded", account.StateActive, gibibyte, 0), map[string]struct{}{"excluded": {}}, false},
		{"exact capacity", candidate("exact", account.StateActive, chunkSize+reserve, 0), nil, true},
		{"below reserve", candidate("small", account.StateActive, chunkSize+reserve-1, 0), nil, false},
		{"invalid usage", candidate("invalid", account.StateActive, gibibyte, gibibyte+1), nil, false},
	}
	rateLimited := candidate("limited", account.StateActive, gibibyte, 0)
	rateLimited.Account.RateLimitedUntil = &future
	tests = append(tests, struct {
		name      string
		candidate Candidate
		excluded  map[string]struct{}
		want      bool
	}{"future rate limit", rateLimited, nil, false})
	expiredLimit := candidate("expired", account.StateActive, gibibyte, 0)
	expiredLimit.Account.RateLimitedUntil = &past
	tests = append(tests, struct {
		name      string
		candidate Candidate
		excluded  map[string]struct{}
		want      bool
	}{"expired rate limit", expiredLimit, nil, true})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewDefault().Select(Request{
				Candidates: []Candidate{test.candidate}, ChunkSizeBytes: chunkSize,
				ExcludedAccountIDs: test.excluded, Now: now,
			})
			if test.want && err != nil {
				t.Fatalf("Select() error = %v", err)
			}
			if !test.want && !errors.Is(err, ErrInsufficientStorage) {
				t.Fatalf("Select() error = %v, want ErrInsufficientStorage", err)
			}
		})
	}
}

func TestScoringComponentsInfluenceSelection(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		candidates []Candidate
		previous   string
		wantID     string
	}{
		{"free capacity", []Candidate{
			candidate("more-free", account.StateActive, gibibyte, 100*mebibyte),
			candidate("less-free", account.StateActive, gibibyte, 500*mebibyte),
		}, "", "more-free"},
		{"priority", withPriorities(0, 10), "", "second"},
		{"load", withLoads(2, 0), "", "second"},
		{"recent errors", withErrors(1, 0), "", "second"},
		{"diversity", equalCandidates(), "first", "second"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection, err := NewDefault().Select(Request{
				Candidates: test.candidates, ChunkSizeBytes: 32 * mebibyte,
				PreviousAccountID: test.previous, Now: now,
			})
			if err != nil {
				t.Fatalf("Select(): %v", err)
			}
			if selection.Candidate.Account.ID != test.wantID {
				t.Fatalf("selected account = %s (score %+v), want %s", selection.Candidate.Account.ID, selection.Score, test.wantID)
			}
		})
	}
}

func TestRetryExclusionSelectsAnotherAccount(t *testing.T) {
	selection, err := NewDefault().Select(Request{
		Candidates: equalCandidates(), ChunkSizeBytes: 32 * mebibyte,
		ExcludedAccountIDs: map[string]struct{}{"first": {}},
	})
	if err != nil {
		t.Fatalf("Select(): %v", err)
	}
	if selection.Candidate.Account.ID != "second" {
		t.Fatalf("selected %s, want second", selection.Candidate.Account.ID)
	}
}

func TestSequentialChunksSpreadAcrossAccounts(t *testing.T) {
	engine := NewDefault()
	candidates := []Candidate{
		candidate("account-a", account.StateActive, gibibyte, 0),
		candidate("account-b", account.StateActive, gibibyte, 0),
		candidate("account-c", account.StateActive, gibibyte, 0),
	}
	selected := make(map[string]int)
	previous := ""
	for range 9 {
		selection, err := engine.Select(Request{Candidates: candidates, ChunkSizeBytes: 32 * mebibyte, PreviousAccountID: previous})
		if err != nil {
			t.Fatalf("Select(): %v", err)
		}
		previous = selection.Candidate.Account.ID
		selected[previous]++
	}
	if len(selected) < 2 {
		t.Fatalf("chunks selected only one account: %v", selected)
	}
}

func TestRankingIsDeterministic(t *testing.T) {
	engine := NewDefault()
	forward := equalCandidates()
	reverse := []Candidate{forward[1], forward[0]}
	for _, candidates := range [][]Candidate{forward, reverse} {
		ranked, err := engine.Rank(Request{Candidates: candidates, ChunkSizeBytes: 32 * mebibyte})
		if err != nil {
			t.Fatalf("Rank(): %v", err)
		}
		if ranked[0].Candidate.Account.ID != "first" {
			t.Fatalf("first ranked account = %s", ranked[0].Candidate.Account.ID)
		}
	}
}

func TestInvalidRequests(t *testing.T) {
	valid := candidate("account", account.StateActive, gibibyte, 0)
	tests := []Request{
		{Candidates: []Candidate{valid}, ChunkSizeBytes: 0},
		{Candidates: []Candidate{valid, valid}, ChunkSizeBytes: 1},
		{Candidates: []Candidate{{Account: valid.Account, UploadsInFlight: -1}}, ChunkSizeBytes: 1},
		{Candidates: []Candidate{{Account: valid.Account, RecentErrorRate: math.NaN()}}, ChunkSizeBytes: 1},
	}
	for index, request := range tests {
		if _, err := NewDefault().Select(request); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("test %d error = %v, want ErrInvalidRequest", index, err)
		}
	}
}

func TestInvalidEngineConfigurationAndReserveOverflow(t *testing.T) {
	config := DefaultConfig()
	config.FreeCapacityWeight = math.NaN()
	if _, err := New(config); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("New() error = %v, want ErrInvalidRequest", err)
	}
	if _, ok := NewDefault().RequiredReserve(math.MaxInt64); ok {
		t.Fatal("RequiredReserve(MaxInt64) reported no overflow")
	}
}

func candidate(id string, state account.State, total, used int64) Candidate {
	return Candidate{Account: account.Account{
		ID: id, State: state, TotalBytes: total, UsedBytes: used,
		FreeBytes: total - used, MaxUploadWorkers: 2,
	}}
}

func equalCandidates() []Candidate {
	return []Candidate{
		candidate("first", account.StateActive, gibibyte, 0),
		candidate("second", account.StateActive, gibibyte, 0),
	}
}

func withPriorities(first, second int) []Candidate {
	values := equalCandidates()
	values[0].Account.Priority = first
	values[1].Account.Priority = second
	return values
}

func withLoads(first, second int) []Candidate {
	values := equalCandidates()
	values[0].UploadsInFlight = first
	values[1].UploadsInFlight = second
	return values
}

func withErrors(first, second float64) []Candidate {
	values := equalCandidates()
	values[0].RecentErrorRate = first
	values[1].RecentErrorRate = second
	return values
}
