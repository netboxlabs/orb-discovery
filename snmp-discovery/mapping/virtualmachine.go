package mapping

import (
	"github.com/netboxlabs/diode-sdk-go/diode"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

// TransformToVirtualMachine rewrites a Device-rooted entity slice into
// a VirtualMachine-rooted one. The returned slice is fresh but
// contains a mix of newly-emitted VirtualMachine/VMInterface entities
// and IPAddress/VLAN entities reused from the input. Input IPAddress
// entities are mutated in place (AssignedObject is rewritten from
// *Interface to *VMInterface) — consistent with PruneNestedRefs and
// other downstream mutations. Caller must not retain the input slice
// expecting it to be unchanged.
//
// Mappings:
//   - *diode.Device           -> *diode.VirtualMachine
//   - *diode.Interface        -> *diode.VMInterface (with VirtualMachine
//                                ref); Parent/Bridge remapped to the
//                                corresponding VMInterface by identity
//                                (with name fallback).
//   - *diode.IPAddress        -> *diode.IPAddress with AssignedObject
//                                rewritten from *Interface to
//                                *VMInterface (identity-first lookup
//                                in the iface map, with a name
//                                fallback). Non-*Interface refs
//                                (*FHRPGroup, etc.) and nil refs pass
//                                through unchanged. IPAddress entity
//                                is mutated in place.
//   - *diode.VLAN             -> passthrough.
//   - *diode.VirtualChassis,
//     *diode.Device with
//     VcPosition != nil,
//     *diode.Module,
//     *diode.ModuleBay        -> dropped (no VM analog).
//
// Cluster on the VM is set only when defaults.Cluster is non-empty.
//
// The caller is responsible for guarding the call site on
// defaults.Type == config.TargetTypeVirtualMachine.
func TransformToVirtualMachine(entities []diode.Entity, defaults *config.Defaults) []diode.Entity {
	if len(entities) == 0 {
		return entities
	}

	// Phase 1: enumerate every *diode.Interface referenced anywhere in
	// the graph and emit one VMInterface per distinct *Interface. The
	// snmp-discovery mapper intentionally suppresses top-level emission
	// of any Interface that is also reachable via IPAddress.AssignedObject
	// (mapping.go::getAssignedInterfaces), so a Device + an IP carrying
	// the only Interface ref is a real input shape. A top-level-only
	// walk would emit zero VMInterfaces and leave the IP carrying an
	// *Interface ref — Codex round-1 BLOCKER.
	var (
		vm            *diode.VirtualMachine
		srcDevice     *diode.Device
		ifaceMap      = make(map[*diode.Interface]*diode.VMInterface, len(entities))
		nameMap       = make(map[string]*diode.VMInterface, len(entities))
		srcIfaceOrder []*diode.Interface // canonical source-Interface insertion order (deterministic)
		topIfaceOrder []*diode.Interface // top-level subset (preserves source order in output)
		ips           []*diode.IPAddress
		vlans         []*diode.VLAN
	)

	ensureVMInterface := func(src *diode.Interface) *diode.VMInterface {
		if src == nil {
			return nil
		}
		if vmIface, ok := ifaceMap[src]; ok {
			return vmIface
		}
		vmIface := interfaceToVMInterface(src)
		ifaceMap[src] = vmIface
		srcIfaceOrder = append(srcIfaceOrder, src) // first-encounter insertion order
		if src.Name != nil && *src.Name != "" {
			// First write wins for the name map; later duplicate names
			// keep the first VMInterface as the by-name resolution target.
			if _, exists := nameMap[*src.Name]; !exists {
				nameMap[*src.Name] = vmIface
			}
		}
		return vmIface
	}

	// First scan: top-level entities. Pins source order for the
	// VMInterface output slice.
	for _, e := range entities {
		switch v := e.(type) {
		case *diode.Device:
			if v == nil || v.VcPosition != nil {
				continue // skip stack members
			}
			if vm == nil {
				srcDevice = v
				vm = deviceToVM(v, defaults)
			}
		case *diode.Interface:
			if v == nil {
				continue
			}
			ensureVMInterface(v)
			topIfaceOrder = append(topIfaceOrder, v)
		case *diode.IPAddress:
			if v != nil {
				ips = append(ips, v)
			}
		case *diode.VLAN:
			if v != nil {
				vlans = append(vlans, v)
			}
			// All other entity types (VirtualChassis, Module, ModuleBay,
			// chassis members reached as nested-only refs, etc.) are
			// dropped on the floor.
		}
	}

	// Second scan: harvest *Interface refs that only exist nested.
	for _, ip := range ips {
		if src, ok := ip.AssignedObject.(*diode.Interface); ok && src != nil {
			ensureVMInterface(src)
		}
	}
	if srcDevice != nil {
		if srcDevice.PrimaryIp4 != nil {
			if src, ok := srcDevice.PrimaryIp4.AssignedObject.(*diode.Interface); ok && src != nil {
				ensureVMInterface(src)
			}
		}
		if srcDevice.PrimaryIp6 != nil {
			if src, ok := srcDevice.PrimaryIp6.AssignedObject.(*diode.Interface); ok && src != nil {
				ensureVMInterface(src)
			}
		}
	}
	// Parent/Bridge may also point at *Interface refs that aren't
	// top-level. Walk to a fixed point so chains deeper than one
	// (A.Parent=B, B.Parent=C) all land in the map. Iterate
	// srcIfaceOrder by index — Go re-evaluates len() each iteration,
	// so newly-appended entries are processed before the loop exits.
	// Terminates because ensureVMInterface only appends for unseen
	// source pointers.
	for i := 0; i < len(srcIfaceOrder); i++ {
		src := srcIfaceOrder[i]
		if src.Parent != nil {
			ensureVMInterface(src.Parent)
		}
		if src.Bridge != nil {
			ensureVMInterface(src.Bridge)
		}
	}

	// Phase 2: attach VM ref + Parent/Bridge remap on every emitted
	// VMInterface.
	for srcIface, vmIface := range ifaceMap {
		vmIface.VirtualMachine = vm
		if srcIface.Parent != nil {
			if mapped, ok := ifaceMap[srcIface.Parent]; ok {
				vmIface.Parent = mapped
			} else if srcIface.Parent.Name != nil {
				if mapped, ok := nameMap[*srcIface.Parent.Name]; ok {
					vmIface.Parent = mapped
				}
			}
		}
		if srcIface.Bridge != nil {
			if mapped, ok := ifaceMap[srcIface.Bridge]; ok {
				vmIface.Bridge = mapped
			} else if srcIface.Bridge.Name != nil {
				if mapped, ok := nameMap[*srcIface.Bridge.Name]; ok {
					vmIface.Bridge = mapped
				}
			}
		}
	}

	// Phase 2b: rebuild the VM's PrimaryIp4/PrimaryIp6 so their
	// AssignedObject points at a *matcher-only VMInterface stub*, not
	// the rich top-level VMInterface that back-references the VM via
	// `VirtualMachine`. Without this we'd create a cycle:
	//   VM -> PrimaryIp4 -> AssignedObject -> VMInterface(rich)
	//         -> VirtualMachine -> VM -> PrimaryIp4 -> ...
	// which crashes the runner's debug-log proto conversion (proto
	// conversion runs BEFORE PruneNestedRefsVM strips nested refs).
	// Codex round-2 caught this; the Device path solves the analogous
	// issue with detachForPrimaryIP in mapping.go.
	rebuildPrimaryIP := func(src *diode.IPAddress) *diode.IPAddress {
		if src == nil {
			return nil
		}
		// Shallow copy so we don't mutate the Device's snapshot in place.
		out := *src
		if iface, ok := out.AssignedObject.(*diode.Interface); ok && iface != nil {
			var vmIfRef *diode.VMInterface
			if mapped, ok := ifaceMap[iface]; ok {
				vmIfRef = mapped
			} else if iface.Name != nil {
				if mapped, ok := nameMap[*iface.Name]; ok {
					vmIfRef = mapped
				}
			}
			if vmIfRef != nil {
				out.AssignedObject = &diode.VMInterface{
					Name:           vmIfRef.Name,
					VirtualMachine: &diode.VirtualMachine{Name: vm.Name},
					// newMACMatchStub (from stubs.go) strips the MAC's
					// AssignedObject so proto conversion can't recurse
					// MAC -> AssignedObject -> ... even though the VM
					// cycle itself is broken.
					PrimaryMacAddress: newMACMatchStub(vmIfRef.PrimaryMacAddress),
				}
			} else {
				// Unresolvable: clear rather than leak the dropped *Interface.
				out.AssignedObject = nil
			}
		}
		return &out
	}
	if vm != nil && srcDevice != nil {
		vm.PrimaryIp4 = rebuildPrimaryIP(srcDevice.PrimaryIp4)
		vm.PrimaryIp6 = rebuildPrimaryIP(srcDevice.PrimaryIp6)
	}

	// Phase 3: rewrite top-level IP AssignedObject from *Interface to
	// *VMInterface (identity-first, name-fallback). Non-*Interface
	// refs (*FHRPGroup, etc.) pass through unchanged.
	for _, ip := range ips {
		if src, ok := ip.AssignedObject.(*diode.Interface); ok && src != nil {
			if mapped, ok := ifaceMap[src]; ok {
				ip.AssignedObject = mapped
			} else if src.Name != nil {
				if mapped, ok := nameMap[*src.Name]; ok {
					ip.AssignedObject = mapped
				}
			}
		}
	}

	// Assemble output. Order: VM, then VMInterfaces in top-level
	// source order first, then any nested-only-harvested VMInterfaces
	// in srcIfaceOrder (deterministic first-encounter), then IPs, then
	// VLANs.
	out := make([]diode.Entity, 0, 1+len(ifaceMap)+len(ips)+len(vlans))
	if vm != nil {
		out = append(out, vm)
	}
	emitted := make(map[*diode.VMInterface]struct{}, len(ifaceMap))
	for _, src := range topIfaceOrder {
		if vmIface, ok := ifaceMap[src]; ok {
			out = append(out, vmIface)
			emitted[vmIface] = struct{}{}
		}
	}
	for _, src := range srcIfaceOrder {
		vmIface := ifaceMap[src]
		if _, already := emitted[vmIface]; already {
			continue
		}
		out = append(out, vmIface)
	}
	for _, ip := range ips {
		out = append(out, ip)
	}
	for _, vlan := range vlans {
		out = append(out, vlan)
	}
	return out
}

func deviceToVM(d *diode.Device, defaults *config.Defaults) *diode.VirtualMachine {
	vm := &diode.VirtualMachine{
		Name:         d.Name,
		Status:       d.Status,
		Serial:       d.Serial,
		Role:         d.Role,
		Platform:     d.Platform,
		Site:         d.Site,
		Tenant:       d.Tenant,
		PrimaryIp4:   d.PrimaryIp4, // rebuilt later in Phase 2b
		PrimaryIp6:   d.PrimaryIp6, // rebuilt later in Phase 2b
		Description:  d.Description,
		Comments:     d.Comments,
		Tags:         d.Tags,
		CustomFields: d.CustomFields,
		Metadata:     d.Metadata,
		Owner:        d.Owner,
	}
	if defaults != nil && defaults.Cluster != "" {
		cluster := defaults.Cluster
		vm.Cluster = &diode.Cluster{Name: &cluster}
	}
	return vm
}

func interfaceToVMInterface(i *diode.Interface) *diode.VMInterface {
	return &diode.VMInterface{
		Name:                  i.Name,
		Enabled:               i.Enabled,
		Mtu:                   i.Mtu,
		PrimaryMacAddress:     i.PrimaryMacAddress,
		Description:           i.Description,
		Mode:                  i.Mode,
		UntaggedVlan:          i.UntaggedVlan,
		QinqSvlan:             i.QinqSvlan,
		VlanTranslationPolicy: i.VlanTranslationPolicy,
		Vrf:                   i.Vrf,
		Tags:                  i.Tags,
		CustomFields:          i.CustomFields,
		TaggedVlans:           i.TaggedVlans,
		Metadata:              i.Metadata,
		Owner:                 i.Owner,
	}
}
