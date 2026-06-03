package policy

import (
	"errors"
	"testing"
	"time"

	"github.com/netboxlabs/diode-sdk-go/diode"
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

func TestGetRunsNewestFirst(t *testing.T) {
	rs := NewRunStore()
	r1 := rs.CreateRun("p1", "h:1")
	rs.UpdateRun("p1", "h:1", r1.ID, RunStatusCompleted, nil, 1)
	time.Sleep(1 * time.Millisecond)
	r2 := rs.CreateRun("p1", "h:1")
	rs.UpdateRun("p1", "h:1", r2.ID, RunStatusCompleted, nil, 2)
	time.Sleep(1 * time.Millisecond)
	r3 := rs.CreateRun("p1", "h:1")
	rs.UpdateRun("p1", "h:1", r3.ID, RunStatusCompleted, nil, 3)

	runs := rs.GetRunsForPolicy("p1")
	require.Len(t, runs, 3)
	require.GreaterOrEqual(t, runs[0].CreatedAt, runs[1].CreatedAt)
	require.GreaterOrEqual(t, runs[1].CreatedAt, runs[2].CreatedAt)
}

func TestAnnotateEntitiesWithRunID(t *testing.T) {
	devName := "router1"
	dev := &diode.Device{
		Name:     &devName,
		Metadata: diode.Metadata{"source_match": "orig"},
	}
	ifName := "Ethernet1"
	iface := &diode.Interface{
		Name:   &ifName,
		Device: dev,
	}
	mod := &diode.Module{Device: dev}
	bay := &diode.ModuleBay{Device: dev}

	entities := []diode.Entity{dev, iface, mod, bay}
	annotateEntitiesWithRunID(entities, "RID")

	// Every top-level entity gets run_id.
	require.Equal(t, "RID", dev.Metadata["run_id"])
	require.Equal(t, "RID", iface.Metadata["run_id"])
	require.Equal(t, "RID", mod.Metadata["run_id"])
	require.Equal(t, "RID", bay.Metadata["run_id"])

	// Nested Device on Interface/Module/ModuleBay also gets run_id.
	require.Equal(t, "RID", iface.Device.Metadata["run_id"])
	require.Equal(t, "RID", mod.Device.Metadata["run_id"])
	require.Equal(t, "RID", bay.Device.Metadata["run_id"])

	// Pre-existing key on Device is preserved.
	require.Equal(t, "orig", dev.Metadata["source_match"])
}
