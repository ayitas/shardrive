package placement

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
)

const mebibyte int64 = 1024 * 1024

var (
	ErrInsufficientStorage = errors.New("insufficient eligible storage")
	ErrInvalidRequest      = errors.New("invalid placement request")
)

type Config struct {
	MinimumReserveBytes    int64
	ReserveChunkMultiplier int64
	FreeCapacityWeight     float64
	PriorityWeight         float64
	LoadPenaltyWeight      float64
	ErrorPenaltyWeight     float64
	DiversityPenalty       float64
}

func DefaultConfig() Config {
	return Config{
		MinimumReserveBytes:    256 * mebibyte,
		ReserveChunkMultiplier: 4,
		FreeCapacityWeight:     0.60,
		PriorityWeight:         0.20,
		LoadPenaltyWeight:      0.15,
		ErrorPenaltyWeight:     0.05,
		DiversityPenalty:       0.10,
	}
}

type Engine struct{ config Config }

func New(config Config) (*Engine, error) {
	if config.MinimumReserveBytes < 0 || config.ReserveChunkMultiplier < 0 ||
		!finiteNonNegative(config.FreeCapacityWeight) || !finiteNonNegative(config.PriorityWeight) ||
		!finiteNonNegative(config.LoadPenaltyWeight) || !finiteNonNegative(config.ErrorPenaltyWeight) ||
		!finiteNonNegative(config.DiversityPenalty) ||
		config.FreeCapacityWeight+config.PriorityWeight == 0 {
		return nil, fmt.Errorf("create placement engine: %w", ErrInvalidRequest)
	}
	return &Engine{config: config}, nil
}

func NewDefault() *Engine {
	return &Engine{config: DefaultConfig()}
}

type Candidate struct {
	Account         account.Account
	UploadsInFlight int
	RecentErrorRate float64
}

type Request struct {
	Candidates         []Candidate
	ChunkSizeBytes     int64
	PreviousAccountID  string
	ExcludedAccountIDs map[string]struct{}
	Now                time.Time
}

type Score struct {
	FreeCapacity     float64
	Priority         float64
	LoadPenalty      float64
	ErrorPenalty     float64
	DiversityPenalty float64
	Total            float64
}

type Selection struct {
	Candidate Candidate
	Score     Score
}

func (e *Engine) Select(request Request) (Selection, error) {
	ranked, err := e.Rank(request)
	if err != nil {
		return Selection{}, err
	}
	return ranked[0], nil
}

func (e *Engine) Rank(request Request) ([]Selection, error) {
	if request.ChunkSizeBytes <= 0 {
		return nil, fmt.Errorf("rank placement candidates: %w: chunk size must be positive", ErrInvalidRequest)
	}
	if request.Now.IsZero() {
		request.Now = time.Now()
	}
	reserve, ok := e.RequiredReserve(request.ChunkSizeBytes)
	if !ok {
		return nil, fmt.Errorf("rank placement candidates: %w: reserve overflows", ErrInvalidRequest)
	}

	eligible := make([]Candidate, 0, len(request.Candidates))
	seen := make(map[string]struct{}, len(request.Candidates))
	for _, candidate := range request.Candidates {
		if candidate.Account.ID == "" {
			return nil, fmt.Errorf("rank placement candidates: %w: empty account ID", ErrInvalidRequest)
		}
		if _, duplicate := seen[candidate.Account.ID]; duplicate {
			return nil, fmt.Errorf("rank placement candidates: %w: duplicate account %s", ErrInvalidRequest, candidate.Account.ID)
		}
		seen[candidate.Account.ID] = struct{}{}
		if candidate.UploadsInFlight < 0 || math.IsNaN(candidate.RecentErrorRate) || math.IsInf(candidate.RecentErrorRate, 0) {
			return nil, fmt.Errorf("rank placement candidates: %w: invalid metrics for account %s", ErrInvalidRequest, candidate.Account.ID)
		}
		if e.eligible(candidate.Account, request, reserve) {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		return nil, ErrInsufficientStorage
	}

	minimumPriority, maximumPriority := eligible[0].Account.Priority, eligible[0].Account.Priority
	for _, candidate := range eligible[1:] {
		if candidate.Account.Priority < minimumPriority {
			minimumPriority = candidate.Account.Priority
		}
		if candidate.Account.Priority > maximumPriority {
			maximumPriority = candidate.Account.Priority
		}
	}
	ranked := make([]Selection, 0, len(eligible))
	for _, candidate := range eligible {
		ranked = append(ranked, Selection{Candidate: candidate, Score: e.score(candidate, minimumPriority, maximumPriority, request.PreviousAccountID)})
	}
	sort.Slice(ranked, func(i, j int) bool { return better(ranked[i], ranked[j]) })
	return ranked, nil
}

func (e *Engine) RequiredReserve(chunkSize int64) (int64, bool) {
	if chunkSize < 0 || (e.config.ReserveChunkMultiplier != 0 && chunkSize > math.MaxInt64/e.config.ReserveChunkMultiplier) {
		return 0, false
	}
	reserve := chunkSize * e.config.ReserveChunkMultiplier
	if reserve < e.config.MinimumReserveBytes {
		reserve = e.config.MinimumReserveBytes
	}
	return reserve, true
}

func (e *Engine) eligible(value account.Account, request Request, reserve int64) bool {
	if value.State != account.StateActive || value.TotalBytes <= 0 || value.UsedBytes < 0 || value.UsedBytes > value.TotalBytes {
		return false
	}
	if _, excluded := request.ExcludedAccountIDs[value.ID]; excluded {
		return false
	}
	if value.RateLimitedUntil != nil && request.Now.Before(*value.RateLimitedUntil) {
		return false
	}
	free := value.TotalBytes - value.UsedBytes
	return free >= request.ChunkSizeBytes && free-request.ChunkSizeBytes >= reserve
}

func (e *Engine) score(candidate Candidate, minimumPriority, maximumPriority int, previousAccountID string) Score {
	freeRatio := clamp(float64(candidate.Account.TotalBytes-candidate.Account.UsedBytes)/float64(candidate.Account.TotalBytes), 0, 1)
	priorityRatio := 0.5
	if maximumPriority != minimumPriority {
		priorityRatio = float64(candidate.Account.Priority-minimumPriority) / float64(maximumPriority-minimumPriority)
	}
	loadRatio := 1.0
	if candidate.Account.MaxUploadWorkers > 0 {
		loadRatio = clamp(float64(candidate.UploadsInFlight)/float64(candidate.Account.MaxUploadWorkers), 0, 1)
	}
	errorRatio := clamp(candidate.RecentErrorRate, 0, 1)
	diversity := 0.0
	if previousAccountID != "" && candidate.Account.ID == previousAccountID {
		diversity = e.config.DiversityPenalty
	}
	result := Score{
		FreeCapacity:     e.config.FreeCapacityWeight * freeRatio,
		Priority:         e.config.PriorityWeight * priorityRatio,
		LoadPenalty:      e.config.LoadPenaltyWeight * loadRatio,
		ErrorPenalty:     e.config.ErrorPenaltyWeight * errorRatio,
		DiversityPenalty: diversity,
	}
	result.Total = result.FreeCapacity + result.Priority - result.LoadPenalty - result.ErrorPenalty - result.DiversityPenalty
	return result
}

func better(left, right Selection) bool {
	if left.Score.Total != right.Score.Total {
		return left.Score.Total > right.Score.Total
	}
	leftFree := left.Candidate.Account.TotalBytes - left.Candidate.Account.UsedBytes
	rightFree := right.Candidate.Account.TotalBytes - right.Candidate.Account.UsedBytes
	if leftFree != rightFree {
		return leftFree > rightFree
	}
	if left.Candidate.Account.Priority != right.Candidate.Account.Priority {
		return left.Candidate.Account.Priority > right.Candidate.Account.Priority
	}
	return left.Candidate.Account.ID < right.Candidate.Account.ID
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
