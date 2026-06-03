package policy

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunStoreLifecycle(t *testing.T) {
	rs := NewRunStore()
	r1 := rs.CreateRun("p1", "10.0.0.1:6030")
	require.NotEmpty(t, r1.ID)
	require.Equal(t, RunStatusRunning, r1.Status)
	rs.UpdateRun("p1", "10.0.0.1:6030", r1.ID, RunStatusCompleted, nil, 7)

	runs := rs.GetRunsForPolicy("p1")
	require.Len(t, runs, 1)
	require.Equal(t, RunStatusCompleted, runs[0].Status)
	require.Equal(t, 7, runs[0].EntityCount)
}

func TestRunStoreKeepsLastN(t *testing.T) {
	rs := NewRunStore()
	for i := 0; i < 5; i++ {
		r := rs.CreateRun("p1", "h:1")
		rs.UpdateRun("p1", "h:1", r.ID, RunStatusFailed, errors.New("x"), 0)
	}
	require.Len(t, rs.GetRunsForPolicy("p1"), maxRunsPerTarget) // capped
}
