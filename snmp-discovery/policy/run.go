package policy

import (
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// RunStatus represents the status of a run
type RunStatus string

const (
	// RunStatusRunning indicates the run is currently running
	RunStatusRunning RunStatus = "running"
	// RunStatusCompleted indicates the run completed successfully
	RunStatusCompleted RunStatus = "completed"
	// RunStatusFailed indicates the run failed with an error
	RunStatusFailed RunStatus = "failed"
)

// Run represents a single run execution
type Run struct {
	ID          string            `json:"id"`
	PolicyID    string            `json:"policy_id"`
	Status      RunStatus         `json:"status"`
	Reason      string            `json:"reason,omitempty"`
	EntityCount int               `json:"entity_count"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// RunStore manages runs in memory with per-target tracking
type RunStore struct {
	mu   sync.RWMutex
	runs map[string]map[string][]*Run // policyName -> target -> runs (max 3 per target)
}

const maxRunsPerTarget = 3

// NewRunStore creates a new RunStore
func NewRunStore() *RunStore {
	return &RunStore{
		runs: make(map[string]map[string][]*Run),
	}
}

// normalizeTarget normalizes target strings to canonical form
func normalizeTarget(target string) string {
	// Try to parse as IP address
	if addr, err := netip.ParseAddr(target); err == nil {
		return addr.String()
	}
	// Try to parse as IP prefix (CIDR)
	if prefix, err := netip.ParsePrefix(target); err == nil {
		return prefix.String()
	}
	// Return as-is for hostnames or other formats
	return target
}

// CreateRun creates a new run for the given policy and target, and returns it
func (rs *RunStore) CreateRun(policyName string, target string, parentTarget string) *Run {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	now := time.Now()

	// Normalize target for consistent storage
	normalizedTarget := normalizeTarget(target)

	// Create metadata with target information
	metadata := make(map[string]string)
	metadata["target"] = target // Store original, not normalized
	if parentTarget != "" {
		metadata["parent_target"] = parentTarget
	}

	run := &Run{
		ID:        uuid.New().String(),
		PolicyID:  policyName,
		Status:    RunStatusRunning,
		Metadata:  metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Initialize policy map if needed
	if rs.runs[policyName] == nil {
		rs.runs[policyName] = make(map[string][]*Run)
	}

	// Add run to the target's run list
	runs := rs.runs[policyName][normalizedTarget]
	runs = append(runs, run)

	// Keep only the last maxRunsPerTarget runs
	if len(runs) > maxRunsPerTarget {
		runs = runs[len(runs)-maxRunsPerTarget:]
	}

	rs.runs[policyName][normalizedTarget] = runs
	return run
}

// UpdateRun updates the status of a run
func (rs *RunStore) UpdateRun(policyName, target, runID string, status RunStatus, err error, entityCount int) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.runs[policyName] == nil {
		return
	}

	// Normalize target for lookup
	normalizedTarget := normalizeTarget(target)

	runs := rs.runs[policyName][normalizedTarget]
	for _, run := range runs {
		if run.ID == runID {
			run.Status = status
			run.EntityCount = entityCount
			run.UpdatedAt = time.Now()
			if err != nil {
				run.Reason = err.Error()
			}
			return
		}
	}
}

// GetRunsForTarget returns all runs for a given policy and target
func (rs *RunStore) GetRunsForTarget(policyName string, target string) []*Run {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if rs.runs[policyName] == nil {
		return nil
	}

	// Normalize target for lookup
	normalizedTarget := normalizeTarget(target)

	runs := rs.runs[policyName][normalizedTarget]
	// Return a copy to avoid race conditions
	result := make([]*Run, len(runs))
	copy(result, runs)
	return result
}

// GetRunsForPolicy returns all runs for a given policy (flattened across all targets)
func (rs *RunStore) GetRunsForPolicy(policyName string) []*Run {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if rs.runs[policyName] == nil {
		return nil
	}

	// Aggregate runs from all targets into a flat list
	var result []*Run
	for _, targetRuns := range rs.runs[policyName] {
		result = append(result, targetRuns...)
	}

	// Sort by CreatedAt descending (newest first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result
}

// GetAllPoliciesWithRuns returns all policies with their runs (flattened per policy)
func (rs *RunStore) GetAllPoliciesWithRuns() map[string][]*Run {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	result := make(map[string][]*Run)
	for policyName, targets := range rs.runs {
		// Flatten all targets' runs into a single array
		var runs []*Run
		for _, targetRuns := range targets {
			runs = append(runs, targetRuns...)
		}

		// Sort runs for consistent ordering (newest first)
		sort.Slice(runs, func(i, j int) bool {
			return runs[i].CreatedAt.After(runs[j].CreatedAt)
		})

		result[policyName] = runs
	}
	return result
}
