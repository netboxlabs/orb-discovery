package policy

import (
	"io"
	"log/slog"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

func sp(s string) *string { return &s }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestPrepareEntitiesForIngest_SourceMatchReachesCycleBreakStub is the
// runner-level guard for the round-2 BLOCKER fix. Any future refactor
// that moves annotateDeviceWithSourceMatch after the transform inside
// prepareEntitiesForIngest must break this test.
//
// The test invokes prepareEntitiesForIngest directly — same code path
// the Runner method uses — with a VM target that has NetboxID set, and
// asserts source_match lands on:
//   - the rich top-level VirtualMachine
//   - the cycle-break stub at vm.PrimaryIp4.AssignedObject.VirtualMachine
//
// Both must match because Diode's matcher resolves the cycle-break stub
// independently from the rich VM under target.netbox_id rediscovery.
func TestPrepareEntitiesForIngest_SourceMatchReachesCycleBreakStub(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	primaryIface := &diode.Interface{Device: dev, Name: sp("eth0")}
	dev.PrimaryIp4 = &diode.IPAddress{
		Address:        sp("192.0.2.1/24"),
		AssignedObject: primaryIface,
	}

	netboxID := 42
	target := config.Target{Host: "192.0.2.1", Port: 161, NetboxID: &netboxID}
	def := &config.Defaults{
		Type:    config.TargetTypeVirtualMachine,
		Cluster: "vyos-cluster",
	}

	out := prepareEntitiesForIngest(
		discardLogger(),
		[]diode.Entity{dev, primaryIface},
		target, def, "run-id-1", "policy-1",
		nil,
	)

	var vm *diode.VirtualMachine
	for _, e := range out {
		if v, ok := e.(*diode.VirtualMachine); ok {
			vm = v
		}
	}
	require.NotNil(t, vm, "missing VirtualMachine in pipeline output")

	// Rich VM has source_match.
	richSM, ok := vm.Metadata["source_match"].(diode.Metadata)
	require.True(t, ok, "rich VM.Metadata[source_match]: got %T, want diode.Metadata", vm.Metadata["source_match"])
	assert.Equal(t, netboxID, richSM["netbox_id"])

	// Cycle-break stub at vm.PrimaryIp4.AssignedObject.VirtualMachine
	// also has source_match — the runner-ordering invariant.
	require.NotNil(t, vm.PrimaryIp4, "vm.PrimaryIp4 missing")
	stubIface, ok := vm.PrimaryIp4.AssignedObject.(*diode.VMInterface)
	require.True(t, ok, "vm.PrimaryIp4.AssignedObject: got %T, want *VMInterface", vm.PrimaryIp4.AssignedObject)
	require.NotNil(t, stubIface.VirtualMachine, "cycle-break stub missing VirtualMachine ref")
	stubSM, ok := stubIface.VirtualMachine.Metadata["source_match"].(diode.Metadata)
	require.True(t, ok,
		"cycle-break stub.Metadata[source_match]: missing — runner ordering broken "+
			"(annotateDeviceWithSourceMatch must run BEFORE TransformToVirtualMachine)")
	assert.Equal(t, netboxID, stubSM["netbox_id"])
}

// TestPrepareEntitiesForIngest_NoNetboxID_NoSourceMatch confirms the
// helper is a no-op for source_match when NetboxID is unset.
func TestPrepareEntitiesForIngest_NoNetboxID_NoSourceMatch(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	target := config.Target{Host: "192.0.2.1", Port: 161} // NetboxID nil
	def := &config.Defaults{Type: config.TargetTypeVirtualMachine, Cluster: "c"}

	out := prepareEntitiesForIngest(discardLogger(), []diode.Entity{dev}, target, def, "run-1", "policy-1", nil)

	for _, e := range out {
		if vm, ok := e.(*diode.VirtualMachine); ok {
			_, hasSM := vm.Metadata["source_match"]
			assert.False(t, hasSM, "VM must NOT carry source_match when target.NetboxID is nil")
		}
	}
}

// TestPrepareEntitiesForIngest_DeviceTargetPathUnchanged checks that
// the Type-unset (Device) path still produces a Device-rooted graph
// with source_match stamped and PruneNestedRefs (not PruneNestedRefsVM)
// invoked. Catches a refactor that accidentally crossed the wires.
func TestPrepareEntitiesForIngest_DeviceTargetPathUnchanged(t *testing.T) {
	dev := &diode.Device{Name: sp("router-1")}
	netboxID := 7
	target := config.Target{Host: "10.0.0.1", Port: 161, NetboxID: &netboxID}
	def := &config.Defaults{} // Type unset → Device target

	out := prepareEntitiesForIngest(discardLogger(), []diode.Entity{dev}, target, def, "run-1", "p", nil)

	// Top-level entity is still a Device (no VM transform fired).
	var richDev *diode.Device
	for _, e := range out {
		if d, ok := e.(*diode.Device); ok {
			richDev = d
		}
		_, isVM := e.(*diode.VirtualMachine)
		assert.False(t, isVM, "Device-target path must not emit *VirtualMachine")
	}
	require.NotNil(t, richDev, "expected a top-level Device in output")
	sm, ok := richDev.Metadata["source_match"].(diode.Metadata)
	require.True(t, ok, "Device.Metadata[source_match]: got %T", richDev.Metadata["source_match"])
	assert.Equal(t, netboxID, sm["netbox_id"])
}

// TestPrepareEntitiesForIngest_RunIDStampedOnPostTransformEntities
// confirms run_id reaches the post-transform VM/VMInterface entities.
// If run_id annotation accidentally ran BEFORE the transform, it would
// land on the source Device (dropped from output) and the VM would
// have nil Metadata for run_id.
func TestPrepareEntitiesForIngest_RunIDStampedOnPostTransformEntities(t *testing.T) {
	dev := &diode.Device{Name: sp("v1")}
	iface := &diode.Interface{Device: dev, Name: sp("eth0")}
	def := &config.Defaults{Type: config.TargetTypeVirtualMachine}

	out := prepareEntitiesForIngest(discardLogger(), []diode.Entity{dev, iface}, config.Target{}, def, "run-xyz", "p", nil)

	var sawVM, sawVMIface bool
	for _, e := range out {
		switch v := e.(type) {
		case *diode.VirtualMachine:
			sawVM = true
			assert.Equal(t, "run-xyz", v.Metadata["run_id"], "VM must carry run_id stamped post-transform")
		case *diode.VMInterface:
			sawVMIface = true
			assert.Equal(t, "run-xyz", v.Metadata["run_id"], "VMInterface must carry run_id stamped post-transform")
		}
	}
	assert.True(t, sawVM, "expected VM in output")
	assert.True(t, sawVMIface, "expected VMInterface in output")
}

// TestPrepareEntitiesForIngest_LogEntitiesCalledBeforePrune asserts the
// logEntities callback fires while nested refs are still rich (a
// reordering that moves it after prune would defeat its debug purpose).
// Assertions run INSIDE the callback because the entities slice is a
// slice of pointers — any post-callback mutation by prune would taint
// a snapshot taken via slice copy.
func TestPrepareEntitiesForIngest_LogEntitiesCalledBeforePrune(t *testing.T) {
	dev := &diode.Device{Name: sp("v1")}
	iface := &diode.Interface{Device: dev, Name: sp("eth0")}

	called := false
	logEntities := func(es []diode.Entity) {
		called = true
		var richVM *diode.VirtualMachine
		for _, e := range es {
			if vm, ok := e.(*diode.VirtualMachine); ok {
				richVM = vm
			}
		}
		require.NotNil(t, richVM, "expected a top-level VM in the logged entities")
		for _, e := range es {
			if v, ok := e.(*diode.VMInterface); ok {
				assert.Same(t, richVM, v.VirtualMachine,
					"VMInterface.VirtualMachine must be the rich VM at log time "+
						"(logEntities must run BEFORE PruneNestedRefsVM rewrites the ref to a stub)")
			}
		}
	}

	def := &config.Defaults{Type: config.TargetTypeVirtualMachine}
	prepareEntitiesForIngest(discardLogger(), []diode.Entity{dev, iface}, config.Target{}, def, "r", "p", logEntities)

	require.True(t, called, "logEntities callback must fire")
}
