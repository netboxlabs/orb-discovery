package policy

import (
	"unsafe"

	"github.com/netboxlabs/diode-sdk-go/diode"
)

// annotateDeviceWithSourceMatch sets source_match metadata on the *diode.Device
// reachable from the entity batch — either at the top level, via Interface.Device,
// or via IPAddress→Interface.Device. This covers the shapes produced by
// MapObjectIDsToEntity; deeper links (Interface.Parent/Bridge/Lag,
// IPAddress.NatInside) are not traversed.
func annotateDeviceWithSourceMatch(entities []diode.Entity, netboxID int) {
	seen := make(map[unsafe.Pointer]struct{})
	for _, e := range entities {
		switch v := e.(type) {
		case *diode.Device:
			setDeviceSourceMatch(v, netboxID, seen)
			// Inline master ref inside a member's VirtualChassis.Master must
			// also carry source_match so Diode's unique_master matcher
			// resolves consistently with the rich master Device.
			if v.VirtualChassis != nil {
				setDeviceSourceMatch(v.VirtualChassis.Master, netboxID, seen)
			}
		case *diode.Interface:
			if v != nil {
				setDeviceSourceMatch(v.Device, netboxID, seen)
			}
		case *diode.IPAddress:
			if v != nil {
				if iface, ok := v.AssignedObject.(*diode.Interface); ok && iface != nil {
					setDeviceSourceMatch(iface.Device, netboxID, seen)
				}
			}
		case *diode.VirtualChassis:
			// Top-level VC carries the master ref that needs source_match
			// for unique_master matcher resolution.
			if v != nil {
				setDeviceSourceMatch(v.Master, netboxID, seen)
			}
		case *diode.VirtualMachine:
			// VM-target path (TransformToVirtualMachine output): stamp
			// netbox_id source_match on the top-level VirtualMachine so
			// target.NetboxID-based re-discovery resolves the existing
			// NetBox VM row instead of creating a new one.
			setVMSourceMatch(v, netboxID, seen)
		}
	}
}

func setDeviceSourceMatch(d *diode.Device, netboxID int, seen map[unsafe.Pointer]struct{}) {
	if d == nil {
		return
	}
	// Skip non-master members emitted by mapping.TranslateAsStack:
	// each carries VcPosition. Annotating them with master's
	// netbox_id would make Diode's source_match matcher collapse
	// every member onto the same NetBox device row.
	if d.VcPosition != nil {
		return
	}
	p := unsafe.Pointer(d)
	if _, ok := seen[p]; ok {
		return
	}
	seen[p] = struct{}{}
	if d.Metadata == nil {
		d.Metadata = make(diode.Metadata)
	}
	d.Metadata["source_match"] = diode.Metadata{"netbox_id": netboxID}
}

// setVMSourceMatch mirrors setDeviceSourceMatch for the VM-target
// path. The top-level entity emitted by TransformToVirtualMachine is
// a *diode.VirtualMachine; this helper stamps the same source_match
// shape so Diode's resolver finds the existing NetBox row on
// re-discovery.
func setVMSourceMatch(vm *diode.VirtualMachine, netboxID int, seen map[unsafe.Pointer]struct{}) {
	if vm == nil {
		return
	}
	p := unsafe.Pointer(vm)
	if _, ok := seen[p]; ok {
		return
	}
	seen[p] = struct{}{}
	if vm.Metadata == nil {
		vm.Metadata = make(diode.Metadata)
	}
	vm.Metadata["source_match"] = diode.Metadata{"netbox_id": netboxID}
}

// annotateEntitiesWithRunID sets per-entity Diode metadata key "run_id" on each entity
// in the batch and on nested Device, Interface, IPAddress, and VLAN references.
func annotateEntitiesWithRunID(entities []diode.Entity, runID string) {
	seen := make(map[unsafe.Pointer]struct{})
	for _, e := range entities {
		switch v := e.(type) {
		case *diode.Device:
			annotateDevice(v, runID, seen)
		case *diode.Interface:
			annotateInterface(v, runID, seen)
		case *diode.IPAddress:
			annotateIPAddress(v, runID, seen)
		case *diode.VLAN:
			annotateVLAN(v, runID, seen)
		case *diode.VirtualChassis:
			annotateVirtualChassis(v, runID, seen)
		case *diode.VirtualMachine:
			annotateVirtualMachine(v, runID, seen)
		case *diode.VMInterface:
			annotateVMInterface(v, runID, seen)
		}
	}
}

func mergeRunID(md *diode.Metadata, runID string) {
	if md == nil {
		return
	}
	if *md == nil {
		*md = make(diode.Metadata)
	}
	(*md)["run_id"] = runID
}

func annotateDevice(d *diode.Device, runID string, seen map[unsafe.Pointer]struct{}) {
	if d == nil {
		return
	}
	p := unsafe.Pointer(d)
	if _, ok := seen[p]; ok {
		return
	}
	seen[p] = struct{}{}
	mergeRunID(&d.Metadata, runID)
}

func annotateInterface(iface *diode.Interface, runID string, seen map[unsafe.Pointer]struct{}) {
	if iface == nil {
		return
	}
	p := unsafe.Pointer(iface)
	if _, ok := seen[p]; ok {
		return
	}
	seen[p] = struct{}{}
	mergeRunID(&iface.Metadata, runID)
	annotateDevice(iface.Device, runID, seen)
	annotateInterface(iface.Parent, runID, seen)
	annotateInterface(iface.Bridge, runID, seen)
	annotateInterface(iface.Lag, runID, seen)
	annotateVLAN(iface.UntaggedVlan, runID, seen)
	for _, v := range iface.TaggedVlans {
		annotateVLAN(v, runID, seen)
	}
}

func annotateIPAddress(ip *diode.IPAddress, runID string, seen map[unsafe.Pointer]struct{}) {
	if ip == nil {
		return
	}
	p := unsafe.Pointer(ip)
	if _, ok := seen[p]; ok {
		return
	}
	seen[p] = struct{}{}
	mergeRunID(&ip.Metadata, runID)
	if ip.AssignedObject != nil {
		switch a := ip.AssignedObject.(type) {
		case *diode.Interface:
			annotateInterface(a, runID, seen)
		case *diode.FHRPGroup:
			mergeRunID(&a.Metadata, runID)
		case *diode.VMInterface:
			// VM-target path: walk fully so the nested VirtualMachine /
			// Parent / Bridge / VLAN refs reached via an IP-assigned
			// VMInterface also get a run_id stamp.
			annotateVMInterface(a, runID, seen)
		}
	}
	if ip.NatInside != nil {
		annotateIPAddress(ip.NatInside, runID, seen)
	}
}

func annotateVirtualChassis(vc *diode.VirtualChassis, runID string, seen map[unsafe.Pointer]struct{}) {
	if vc == nil {
		return
	}
	p := unsafe.Pointer(vc)
	if _, ok := seen[p]; ok {
		return
	}
	seen[p] = struct{}{}
	mergeRunID(&vc.Metadata, runID)
	// Stamp run_id on the inline Master Device stub so it lines up with
	// the run_id on the rich top-level Device that the stub matches —
	// keeps annotation consistent with how VLAN and Interface
	// annotation reach their nested Device refs. annotateDevice only
	// mutates d.Metadata (no recursion into Device's nested fields), so
	// no cycle risk through Master.VirtualChassis.
	if vc.Master != nil {
		annotateDevice(vc.Master, runID, seen)
	}
}

func annotateVLAN(vlan *diode.VLAN, runID string, seen map[unsafe.Pointer]struct{}) {
	if vlan == nil {
		return
	}
	p := unsafe.Pointer(vlan)
	if _, ok := seen[p]; ok {
		return
	}
	seen[p] = struct{}{}
	mergeRunID(&vlan.Metadata, runID)
}

// annotateVirtualMachine stamps run_id on the VM and recurses into
// nested refs that carry their own Metadata (PrimaryIp4/6 — which can
// reach an IP-assigned VMInterface via annotateIPAddress, which is
// fine because the walker dedup-set breaks the cycle).
func annotateVirtualMachine(vm *diode.VirtualMachine, runID string, seen map[unsafe.Pointer]struct{}) {
	if vm == nil {
		return
	}
	p := unsafe.Pointer(vm)
	if _, ok := seen[p]; ok {
		return
	}
	seen[p] = struct{}{}
	mergeRunID(&vm.Metadata, runID)
	annotateIPAddress(vm.PrimaryIp4, runID, seen)
	annotateIPAddress(vm.PrimaryIp6, runID, seen)
}

// annotateVMInterface stamps run_id on the VMInterface and recurses
// into nested refs (VirtualMachine, Parent, Bridge, UntaggedVlan,
// QinqSvlan, TaggedVlans). Mirrors annotateInterface for the
// Device-rooted graph.
func annotateVMInterface(vmIface *diode.VMInterface, runID string, seen map[unsafe.Pointer]struct{}) {
	if vmIface == nil {
		return
	}
	p := unsafe.Pointer(vmIface)
	if _, ok := seen[p]; ok {
		return
	}
	seen[p] = struct{}{}
	mergeRunID(&vmIface.Metadata, runID)
	annotateVirtualMachine(vmIface.VirtualMachine, runID, seen)
	annotateVMInterface(vmIface.Parent, runID, seen)
	annotateVMInterface(vmIface.Bridge, runID, seen)
	annotateVLAN(vmIface.UntaggedVlan, runID, seen)
	annotateVLAN(vmIface.QinqSvlan, runID, seen)
	for _, vlan := range vmIface.TaggedVlans {
		annotateVLAN(vlan, runID, seen)
	}
}
