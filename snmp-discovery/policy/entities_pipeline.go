package policy

import (
	"log/slog"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/mapping"
)

// prepareEntitiesForIngest applies the post-queryTarget pipeline:
// source_match annotation, optional Device→VM transform, run_id
// annotation, the optional debug-log of every entity, and finally
// nested-ref pruning. The Runner method calls this helper rather than
// inlining the four steps so any future reordering shows up here AND
// in TestPrepareEntitiesForIngest_*. Callers that bypass this helper
// (e.g. ad-hoc inlining) lose the protection the test provides.
//
// Ordering invariants — DO NOT REORDER without updating the tests:
//
//  1. annotateDeviceWithSourceMatch FIRST, on the Device-rooted graph.
//     The VM transform's deviceToVM does `Metadata: d.Metadata`
//     (pointer-share) and rebuildPrimaryIP's newVMMatchStub captures
//     Metadata["source_match"] by value at construction time. Stamping
//     source_match AFTER the transform would leave the cycle-break
//     stub at vm.PrimaryIp4.AssignedObject.VirtualMachine missing
//     source_match — breaking the NetBox plugin matcher path under
//     target.netbox_id rediscovery.
//
//  2. TransformToVirtualMachine SECOND (only when Type ==
//     TargetTypeVirtualMachine). Rewrites Device→VM, Interface→
//     VMInterface, etc.
//
//  3. annotateEntitiesWithRunID THIRD, on the post-transform graph so
//     the walker stamps run_id on the VM/VMInterface entities the
//     ingest actually carries (the source Device is no longer in the
//     output entities slice).
//
//  4. logEntities FOURTH (if non-nil), with the rich shared graph
//     visible — easier to debug than the post-prune stubbed payload.
//
//  5. PruneNestedRefs / PruneNestedRefsVM LAST. Runs after annotation
//     so the annotators can walk the rich shared graph with their
//     unsafe.Pointer dedup intact — otherwise every stub would need
//     its own metadata pass.
//
// `logger` may be nil; the transform debug message is then skipped.
// `logEntities` may be nil; the per-entity dump is then skipped.
func prepareEntitiesForIngest(
	logger *slog.Logger,
	entities []diode.Entity,
	target config.Target,
	def *config.Defaults,
	runID, policyName string,
	logEntities func([]diode.Entity),
) []diode.Entity {
	isVM := def != nil && strings.EqualFold(def.Type, config.TargetTypeVirtualMachine)

	if target.NetboxID != nil {
		annotateDeviceWithSourceMatch(entities, *target.NetboxID)
	}

	if isVM {
		entities = mapping.TransformToVirtualMachine(entities, def)
		if logger != nil {
			logger.Debug("transformed entities to VirtualMachine graph",
				"host", target.Host, "policy", policyName, "entity_count", len(entities))
		}
	}

	annotateEntitiesWithRunID(entities, runID)

	if logEntities != nil {
		logEntities(entities)
	}

	if isVM {
		mapping.PruneNestedRefsVM(entities, mapping.CurrentVirtualMachineFrom(entities))
	} else {
		mapping.PruneNestedRefs(entities, mapping.CurrentDeviceFrom(entities))
	}

	return entities
}
