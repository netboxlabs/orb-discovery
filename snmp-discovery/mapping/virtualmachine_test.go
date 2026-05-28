package mapping_test

import (
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/mapping"
)

func sp(s string) *string { return &s }
func bp(b bool) *bool     { return &b }
func ip64(i int64) *int64 { return &i }

func TestTransformToVirtualMachine_DeviceToVM(t *testing.T) {
	dev := &diode.Device{
		Name:        sp("vyos-edge-1"),
		Status:      sp("active"),
		Serial:      sp("ABC123"),
		Role:        &diode.DeviceRole{Name: sp("Router")},
		Platform:    &diode.Platform{Name: sp("VyOS 1.4")},
		Site:        &diode.Site{Name: sp("Dell Lab Site")},
		Tenant:      &diode.Tenant{Name: sp("NetOps")},
		Description: sp("VyOS edge router"),
		Comments:    sp("ingest"),
		DeviceType:  &diode.DeviceType{Model: sp("CSR")},     // must be dropped
		Location:    &diode.Location{Name: sp("rack-1")},     // must be dropped
		AssetTag:    sp("asset-1"),                           // must be dropped
	}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev},
		&config.Defaults{Type: config.TargetTypeVirtualMachine, Cluster: "proxmox-a"},
	)
	if len(out) != 1 {
		t.Fatalf("entity count: got %d, want 1", len(out))
	}
	vm, ok := out[0].(*diode.VirtualMachine)
	if !ok {
		t.Fatalf("entity type: got %T, want *diode.VirtualMachine", out[0])
	}
	if vm.Name == nil || *vm.Name != "vyos-edge-1" {
		t.Errorf("VM.Name: got %v", vm.Name)
	}
	if vm.Status == nil || *vm.Status != "active" {
		t.Errorf("VM.Status: got %v", vm.Status)
	}
	if vm.Serial == nil || *vm.Serial != "ABC123" {
		t.Errorf("VM.Serial: got %v", vm.Serial)
	}
	if vm.Role == nil || vm.Role.Name == nil || *vm.Role.Name != "Router" {
		t.Errorf("VM.Role: got %v", vm.Role)
	}
	if vm.Platform == nil || vm.Platform.Name == nil || *vm.Platform.Name != "VyOS 1.4" {
		t.Errorf("VM.Platform: got %v", vm.Platform)
	}
	if vm.Site == nil || vm.Site.Name == nil || *vm.Site.Name != "Dell Lab Site" {
		t.Errorf("VM.Site: got %v", vm.Site)
	}
	if vm.Tenant == nil || vm.Tenant.Name == nil || *vm.Tenant.Name != "NetOps" {
		t.Errorf("VM.Tenant: got %v", vm.Tenant)
	}
	if vm.Description == nil || *vm.Description != "VyOS edge router" {
		t.Errorf("VM.Description: got %v", vm.Description)
	}
	if vm.Comments == nil || *vm.Comments != "ingest" {
		t.Errorf("VM.Comments: got %v", vm.Comments)
	}
	if vm.Cluster == nil || vm.Cluster.Name == nil || *vm.Cluster.Name != "proxmox-a" {
		t.Errorf("VM.Cluster: got %v", vm.Cluster)
	}
}

func TestTransformToVirtualMachine_DeviceFieldCarryAll(t *testing.T) {
	tag := &diode.Tag{Name: sp("env-prod")}
	owner := &diode.Owner{Name: sp("netops")}
	primary4 := &diode.IPAddress{Address: sp("192.0.2.1/24")}
	primary6 := &diode.IPAddress{Address: sp("2001:db8::1/64")}
	dev := &diode.Device{
		Name:         sp("vyos-edge-1"),
		Tenant:       &diode.Tenant{Name: sp("NetOps")},
		PrimaryIp4:   primary4,
		PrimaryIp6:   primary6,
		Tags:         []*diode.Tag{tag},
		CustomFields: map[string]*diode.CustomFieldValue{"k": {}},
		Owner:        owner,
	}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	vm := out[0].(*diode.VirtualMachine)
	if vm.Tenant == nil || vm.Tenant.Name == nil || *vm.Tenant.Name != "NetOps" {
		t.Errorf("VM.Tenant: got %v", vm.Tenant)
	}
	if vm.PrimaryIp4 == nil || vm.PrimaryIp4.Address == nil || *vm.PrimaryIp4.Address != "192.0.2.1/24" {
		t.Errorf("VM.PrimaryIp4: got %v", vm.PrimaryIp4)
	}
	if vm.PrimaryIp6 == nil || vm.PrimaryIp6.Address == nil || *vm.PrimaryIp6.Address != "2001:db8::1/64" {
		t.Errorf("VM.PrimaryIp6: got %v", vm.PrimaryIp6)
	}
	if len(vm.Tags) != 1 || vm.Tags[0] != tag {
		t.Errorf("VM.Tags: got %v", vm.Tags)
	}
	if _, ok := vm.CustomFields["k"]; !ok {
		t.Errorf("VM.CustomFields: missing 'k'; got %v", vm.CustomFields)
	}
	if vm.Owner != owner {
		t.Errorf("VM.Owner: got %p, want %p", vm.Owner, owner)
	}
}

func TestTransformToVirtualMachine_InterfaceToVMInterface(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	iface := &diode.Interface{
		Device:            dev,
		Name:              sp("eth0"),
		Enabled:           bp(true),
		Mtu:               ip64(1500),
		PrimaryMacAddress: &diode.MACAddress{MacAddress: sp("00:11:22:33:44:55")},
		Description:       sp("WAN"),
		Mode:              sp("access"),
		UntaggedVlan:      &diode.VLAN{Vid: ip64(10)},
		Vrf:               &diode.VRF{Name: sp("default")},
		Speed:             ip64(1000000),       // must be dropped (no VMInterface.Speed)
		Type:              sp("1000base-t"),    // must be dropped
		Lag:               &diode.Interface{Name: sp("bond0")}, // must be dropped
	}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, iface},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	var vmIface *diode.VMInterface
	var vm *diode.VirtualMachine
	for _, e := range out {
		switch v := e.(type) {
		case *diode.VMInterface:
			vmIface = v
		case *diode.VirtualMachine:
			vm = v
		}
	}
	if vm == nil {
		t.Fatal("expected a VirtualMachine entity in the output")
	}
	if vmIface == nil {
		t.Fatal("expected a VMInterface entity in the output")
	}
	if vmIface.Name == nil || *vmIface.Name != "eth0" {
		t.Errorf("VMInterface.Name: got %v", vmIface.Name)
	}
	if vmIface.VirtualMachine != vm {
		t.Errorf("VMInterface.VirtualMachine: got %p, want the emitted VM %p", vmIface.VirtualMachine, vm)
	}
	if vmIface.Mtu == nil || *vmIface.Mtu != 1500 {
		t.Errorf("VMInterface.Mtu: got %v", vmIface.Mtu)
	}
	if vmIface.PrimaryMacAddress == nil {
		t.Errorf("VMInterface.PrimaryMacAddress: lost during transform")
	}
	if vmIface.UntaggedVlan == nil {
		t.Errorf("VMInterface.UntaggedVlan: lost during transform")
	}
	if vmIface.Vrf == nil {
		t.Errorf("VMInterface.Vrf: lost during transform")
	}
	if vmIface.Mode == nil || *vmIface.Mode != "access" {
		t.Errorf("VMInterface.Mode: got %v", vmIface.Mode)
	}
}

func TestTransformToVirtualMachine_InterfaceFieldCarryAll(t *testing.T) {
	tag := &diode.Tag{Name: sp("acl-trunk")}
	owner := &diode.Owner{Name: sp("netops")}
	vtp := &diode.VLANTranslationPolicy{Name: sp("vtp-1")}
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	iface := &diode.Interface{
		Device:                dev,
		Name:                  sp("eth0"),
		Tags:                  []*diode.Tag{tag},
		CustomFields:          map[string]*diode.CustomFieldValue{"role": {}},
		Owner:                 owner,
		VlanTranslationPolicy: vtp,
	}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, iface},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	var vmIface *diode.VMInterface
	for _, e := range out {
		if v, ok := e.(*diode.VMInterface); ok {
			vmIface = v
		}
	}
	if vmIface == nil {
		t.Fatal("missing VMInterface")
	}
	if len(vmIface.Tags) != 1 || vmIface.Tags[0] != tag {
		t.Errorf("VMInterface.Tags: got %v", vmIface.Tags)
	}
	if _, ok := vmIface.CustomFields["role"]; !ok {
		t.Errorf("VMInterface.CustomFields: missing 'role'; got %v", vmIface.CustomFields)
	}
	if vmIface.Owner != owner {
		t.Errorf("VMInterface.Owner: got %p, want %p", vmIface.Owner, owner)
	}
	if vmIface.VlanTranslationPolicy != vtp {
		t.Errorf("VMInterface.VlanTranslationPolicy: got %p, want %p", vmIface.VlanTranslationPolicy, vtp)
	}
}

func TestTransformToVirtualMachine_IPReanchoredOntoVMInterface(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	iface := &diode.Interface{Device: dev, Name: sp("eth0")}
	ip := &diode.IPAddress{
		Address:        sp("192.0.2.1/24"),
		AssignedObject: iface,
	}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, iface, ip},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	var vmIface *diode.VMInterface
	var ipOut *diode.IPAddress
	for _, e := range out {
		switch v := e.(type) {
		case *diode.VMInterface:
			vmIface = v
		case *diode.IPAddress:
			ipOut = v
		}
	}
	if vmIface == nil || ipOut == nil {
		t.Fatalf("missing entity: vmIface=%p ipOut=%p", vmIface, ipOut)
	}
	assigned, ok := ipOut.AssignedObject.(*diode.VMInterface)
	if !ok {
		t.Fatalf("IPAddress.AssignedObject: got %T, want *diode.VMInterface", ipOut.AssignedObject)
	}
	if assigned != vmIface {
		t.Errorf("IP not re-anchored onto the emitted VMInterface (got %p, want %p)", assigned, vmIface)
	}
}

func TestTransformToVirtualMachine_IPOnlyNestedInterface(t *testing.T) {
	// Codex round-1 BLOCKER regression: the mapper filters
	// IP-assigned interfaces out of the top-level slice, so an
	// *Interface can exist only as IPAddress.AssignedObject. The
	// transform must still emit a VMInterface for it and re-anchor
	// the IP.
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	hiddenIface := &diode.Interface{Device: dev, Name: sp("eth-hidden")}
	ip := &diode.IPAddress{Address: sp("203.0.113.5/32"), AssignedObject: hiddenIface}
	// Note: hiddenIface is NOT in the top-level slice.
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, ip},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	var vmIface *diode.VMInterface
	var ipOut *diode.IPAddress
	for _, e := range out {
		switch v := e.(type) {
		case *diode.VMInterface:
			vmIface = v
		case *diode.IPAddress:
			ipOut = v
		}
	}
	if vmIface == nil {
		t.Fatal("expected a VMInterface to be emitted from the IP-only nested *Interface")
	}
	if vmIface.Name == nil || *vmIface.Name != "eth-hidden" {
		t.Errorf("VMInterface.Name: got %v, want eth-hidden", vmIface.Name)
	}
	if ipOut == nil {
		t.Fatal("IPAddress was dropped")
	}
	assigned, ok := ipOut.AssignedObject.(*diode.VMInterface)
	if !ok || assigned != vmIface {
		t.Errorf("IP not re-anchored: got %T %p, want %p", ipOut.AssignedObject, ipOut.AssignedObject, vmIface)
	}
}

func TestTransformToVirtualMachine_IPWithDistinctPointerSameNameEmitsTwoVMInterfaces(t *testing.T) {
	// When the IP's AssignedObject is a *Interface that's a different
	// pointer than the top-level *Interface with the same Name, Phase
	// 1's IP-assigned-iface harvest adds BOTH source pointers to
	// ifaceMap as distinct entries. Identity lookup in Phase 3 resolves
	// the IP to the harvested VMInterface (not the top-level one).
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	topIface := &diode.Interface{Device: dev, Name: sp("eth0")}
	differentPointerSameName := &diode.Interface{Device: dev, Name: sp("eth0")}
	ip := &diode.IPAddress{Address: sp("192.0.2.1/24"), AssignedObject: differentPointerSameName}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, topIface, ip},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	vmIfsByName := map[string][]*diode.VMInterface{}
	var ipOut *diode.IPAddress
	for _, e := range out {
		switch v := e.(type) {
		case *diode.VMInterface:
			if v.Name != nil {
				vmIfsByName[*v.Name] = append(vmIfsByName[*v.Name], v)
			}
		case *diode.IPAddress:
			ipOut = v
		}
	}
	if len(vmIfsByName["eth0"]) != 2 {
		t.Fatalf("expected two distinct eth0 VMInterfaces from the two distinct source pointers; got %d", len(vmIfsByName["eth0"]))
	}
	assigned, ok := ipOut.AssignedObject.(*diode.VMInterface)
	if !ok {
		t.Fatalf("IP.AssignedObject: got %T, want *VMInterface", ipOut.AssignedObject)
	}
	// Top-level eth0 lands earlier than the nested-only harvested one.
	if assigned == vmIfsByName["eth0"][0] {
		t.Errorf("IP resolved via identity to the top-level VMInterface; expected the harvested one (from the distinct source pointer)")
	}
	if assigned != vmIfsByName["eth0"][1] {
		t.Errorf("IP.AssignedObject: got %p, want the harvested (second) VMInterface %p", assigned, vmIfsByName["eth0"][1])
	}
}

func TestTransformToVirtualMachine_IPWithNonInterfaceAssignmentPassesThrough(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	fhrp := &diode.FHRPGroup{GroupId: ip64(42)}
	ip := &diode.IPAddress{Address: sp("192.0.2.1/24"), AssignedObject: fhrp}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, ip},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	var ipOut *diode.IPAddress
	for _, e := range out {
		if v, ok := e.(*diode.IPAddress); ok {
			ipOut = v
		}
	}
	if ipOut == nil {
		t.Fatal("IPAddress missing from output")
	}
	got, ok := ipOut.AssignedObject.(*diode.FHRPGroup)
	if !ok {
		t.Fatalf("IP.AssignedObject: got %T, want *FHRPGroup (untouched passthrough)", ipOut.AssignedObject)
	}
	if got != fhrp {
		t.Errorf("FHRPGroup pointer should be identity-equal to input; got %p, want %p", got, fhrp)
	}
}

func TestTransformToVirtualMachine_VLANPassthrough(t *testing.T) {
	vlan := &diode.VLAN{Vid: ip64(100), Name: sp("users")}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{vlan},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	if len(out) != 1 {
		t.Fatalf("entity count: got %d, want 1", len(out))
	}
	got, ok := out[0].(*diode.VLAN)
	if !ok {
		t.Fatalf("VLAN passthrough type: got %T", out[0])
	}
	if got != vlan {
		t.Errorf("VLAN passthrough identity: got %p, want %p", got, vlan)
	}
}

func TestTransformToVirtualMachine_DropsChassisAndVC(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	vc := &diode.VirtualChassis{Name: sp("vc-1")}
	memberDev := &diode.Device{Name: sp("vyos-edge-1-2"), VcPosition: ip64(2), VirtualChassis: vc}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, vc, memberDev},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	for _, e := range out {
		switch e.(type) {
		case *diode.VirtualChassis:
			t.Errorf("VirtualChassis should be dropped, got %v", e)
		case *diode.Device:
			t.Errorf("All *diode.Device should be transformed/dropped, got %v", e)
		}
	}
}

func TestTransformToVirtualMachine_ModulesDropped(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	mod := &diode.Module{Serial: sp("mod-1")}
	bay := &diode.ModuleBay{Name: sp("bay-1")}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, mod, bay},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	for _, e := range out {
		switch e.(type) {
		case *diode.Module:
			t.Errorf("Module should be dropped: got %v", e)
		case *diode.ModuleBay:
			t.Errorf("ModuleBay should be dropped: got %v", e)
		}
	}
}

func TestTransformToVirtualMachine_ClusterOnlyWhenConfigured(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev},
		&config.Defaults{Type: config.TargetTypeVirtualMachine, Cluster: ""},
	)
	vm := out[0].(*diode.VirtualMachine)
	if vm.Cluster != nil {
		t.Errorf("VM.Cluster: got %v, want nil when defaults.cluster is empty", vm.Cluster)
	}
}

func TestTransformToVirtualMachine_ClusterTypeSetWhenConfigured(t *testing.T) {
	// When defaults.cluster_type is set alongside defaults.cluster,
	// the emitted Cluster carries a Type — required by NetBox's
	// virtualization.cluster model for on-the-fly cluster creation.
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev},
		&config.Defaults{
			Type:        config.TargetTypeVirtualMachine,
			Cluster:     "proxmox-a",
			ClusterType: "Proxmox",
		},
	)
	vm := out[0].(*diode.VirtualMachine)
	if vm.Cluster == nil || vm.Cluster.Name == nil || *vm.Cluster.Name != "proxmox-a" {
		t.Fatalf("VM.Cluster: got %v, want Name=proxmox-a", vm.Cluster)
	}
	if vm.Cluster.Type == nil || vm.Cluster.Type.Name == nil || *vm.Cluster.Type.Name != "Proxmox" {
		t.Errorf("VM.Cluster.Type: got %v, want Name=Proxmox (required by NetBox for cluster auto-create)", vm.Cluster.Type)
	}
}

func TestTransformToVirtualMachine_ClusterTypeOmittedWhenUnset(t *testing.T) {
	// defaults.cluster set, defaults.cluster_type unset: Cluster is
	// emitted as reference-only (no Type). This relies on the cluster
	// already existing in NetBox — the Diode reconciler resolves by
	// name without needing a Type for an existing cluster.
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev},
		&config.Defaults{
			Type:    config.TargetTypeVirtualMachine,
			Cluster: "pre-existing-cluster",
		},
	)
	vm := out[0].(*diode.VirtualMachine)
	if vm.Cluster == nil || vm.Cluster.Name == nil || *vm.Cluster.Name != "pre-existing-cluster" {
		t.Fatalf("VM.Cluster: got %v, want Name=pre-existing-cluster", vm.Cluster)
	}
	if vm.Cluster.Type != nil {
		t.Errorf("VM.Cluster.Type: got %v, want nil when defaults.cluster_type is empty", vm.Cluster.Type)
	}
}

func TestTransformToVirtualMachine_PrimaryIPRebuilt(t *testing.T) {
	cluster := &diode.Cluster{Name: sp("vyos-cluster")}
	dev := &diode.Device{Name: sp("vyos-edge-1"), Cluster: cluster}
	primaryIface := &diode.Interface{Device: dev, Name: sp("eth0")}
	primaryIP := &diode.IPAddress{Address: sp("192.0.2.1/24"), AssignedObject: primaryIface}
	dev.PrimaryIp4 = primaryIP
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, primaryIface},
		&config.Defaults{Type: config.TargetTypeVirtualMachine, Cluster: "vyos-cluster"},
	)
	var vm *diode.VirtualMachine
	var vmIface *diode.VMInterface
	for _, e := range out {
		switch v := e.(type) {
		case *diode.VirtualMachine:
			vm = v
		case *diode.VMInterface:
			vmIface = v
		}
	}
	if vm == nil || vmIface == nil {
		t.Fatalf("missing entity: vm=%p vmIface=%p", vm, vmIface)
	}
	if vm.PrimaryIp4 == nil {
		t.Fatal("VM.PrimaryIp4: got nil, want a rebuilt snapshot")
	}
	if vm.PrimaryIp4 == primaryIP {
		t.Error("VM.PrimaryIp4: got the original Device snapshot pointer; want a fresh copy")
	}
	rebuiltAssigned, ok := vm.PrimaryIp4.AssignedObject.(*diode.VMInterface)
	if !ok {
		t.Fatalf("VM.PrimaryIp4.AssignedObject: got %T, want *diode.VMInterface", vm.PrimaryIp4.AssignedObject)
	}
	if rebuiltAssigned == vmIface {
		t.Errorf("VM.PrimaryIp4.AssignedObject: aliased the rich top-level VMInterface, which would form a VM<->VMInterface cycle")
	}
	if rebuiltAssigned.Name == nil || *rebuiltAssigned.Name != "eth0" {
		t.Errorf("VM.PrimaryIp4.AssignedObject.Name: got %v", rebuiltAssigned.Name)
	}
	stubVM := rebuiltAssigned.VirtualMachine
	if stubVM == nil {
		t.Fatal("VM.PrimaryIp4.AssignedObject.VirtualMachine: got nil, want matcher stub")
	}
	if stubVM == vm {
		t.Errorf("VM.PrimaryIp4.AssignedObject.VirtualMachine: aliased the rich VM, cycle still present")
	}
	if stubVM.Name == nil || *stubVM.Name != "vyos-edge-1" {
		t.Errorf("VM.PrimaryIp4.AssignedObject.VirtualMachine.Name: got %v, want vyos-edge-1", stubVM.Name)
	}
	// The stub must carry NetBox VM-matcher fields (name+cluster) so
	// the reconciler resolves to the right VM. Cluster was missing on
	// the original name-only stub and broke matching when VM names
	// were unique only within a cluster.
	if stubVM.Cluster == nil {
		t.Error("stubVM.Cluster: got nil, want matcher field carried from rich VM")
	}
	// Cycle-safety: stubVM.PrimaryIp4 may be a matcher IP, but its
	// AssignedObject MUST be nil so the chain terminates one hop
	// deeper. newIPMatchStub enforces this.
	if stubVM.PrimaryIp4 != nil && stubVM.PrimaryIp4.AssignedObject != nil {
		t.Errorf("stubVM.PrimaryIp4.AssignedObject: got %T, want nil (matcher-only IP, cycle must terminate)", stubVM.PrimaryIp4.AssignedObject)
	}
	if stubVM.PrimaryIp6 != nil && stubVM.PrimaryIp6.AssignedObject != nil {
		t.Errorf("stubVM.PrimaryIp6.AssignedObject: got %T, want nil (matcher-only IP, cycle must terminate)", stubVM.PrimaryIp6.AssignedObject)
	}
}

func TestTransformToVirtualMachine_PrimaryIPHarvestedDuringRebuild(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	nestedOnly := &diode.Interface{Device: dev, Name: sp("nested-only")}
	dev.PrimaryIp4 = &diode.IPAddress{Address: sp("192.0.2.1/24"), AssignedObject: nestedOnly}
	// nestedOnly NOT in the top-level slice.
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	var vm *diode.VirtualMachine
	for _, e := range out {
		if v, ok := e.(*diode.VirtualMachine); ok {
			vm = v
		}
	}
	if vm == nil || vm.PrimaryIp4 == nil {
		t.Fatalf("VM/PrimaryIp4 missing: vm=%p", vm)
	}
	assigned, ok := vm.PrimaryIp4.AssignedObject.(*diode.VMInterface)
	if !ok {
		t.Fatalf("VM.PrimaryIp4.AssignedObject: got %T, want *diode.VMInterface (resolved via primary-IP harvest)", vm.PrimaryIp4.AssignedObject)
	}
	if assigned.Name == nil || *assigned.Name != "nested-only" {
		t.Errorf("harvested stub Name: got %v, want nested-only", assigned.Name)
	}
}

func TestTransformToVirtualMachine_PrimaryIPNilAssignmentPreserved(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	dev.PrimaryIp4 = &diode.IPAddress{
		Address:        sp("192.0.2.1/24"),
		AssignedObject: nil,
	}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	var vm *diode.VirtualMachine
	for _, e := range out {
		if v, ok := e.(*diode.VirtualMachine); ok {
			vm = v
		}
	}
	if vm == nil || vm.PrimaryIp4 == nil {
		t.Fatalf("VM/PrimaryIp4 missing: vm=%p", vm)
	}
	if vm.PrimaryIp4.AssignedObject != nil {
		t.Errorf("VM.PrimaryIp4.AssignedObject: got %T, want nil (passthrough)", vm.PrimaryIp4.AssignedObject)
	}
}

func TestTransformToVirtualMachine_NoVMCycleAfterTransform(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	primaryIface := &diode.Interface{Device: dev, Name: sp("eth0")}
	dev.PrimaryIp4 = &diode.IPAddress{Address: sp("192.0.2.1/24"), AssignedObject: primaryIface}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, primaryIface},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	var vm *diode.VirtualMachine
	for _, e := range out {
		if v, ok := e.(*diode.VirtualMachine); ok {
			vm = v
		}
	}
	if vm == nil || vm.PrimaryIp4 == nil {
		t.Fatal("missing VM/PrimaryIp4")
	}
	assigned := vm.PrimaryIp4.AssignedObject.(*diode.VMInterface)
	if assigned.VirtualMachine == vm {
		t.Errorf("cycle: VM.PrimaryIp4.AssignedObject.VirtualMachine aliases the rich VM")
	}
}

func TestTransformToVirtualMachine_PrimaryIPStubMACIsMatcherOnly(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	macAssigned := &diode.Interface{Device: dev, Name: sp("eth0")}
	mac := &diode.MACAddress{
		MacAddress:     sp("00:11:22:33:44:55"),
		AssignedObject: macAssigned, // recursion if not stripped
	}
	primaryIface := &diode.Interface{Device: dev, Name: sp("eth0"), PrimaryMacAddress: mac}
	dev.PrimaryIp4 = &diode.IPAddress{Address: sp("192.0.2.1/24"), AssignedObject: primaryIface}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, primaryIface},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	var vm *diode.VirtualMachine
	for _, e := range out {
		if v, ok := e.(*diode.VirtualMachine); ok {
			vm = v
		}
	}
	if vm == nil || vm.PrimaryIp4 == nil {
		t.Fatal("missing VM/PrimaryIp4")
	}
	assigned := vm.PrimaryIp4.AssignedObject.(*diode.VMInterface)
	if assigned.PrimaryMacAddress == nil {
		t.Fatal("cycle-break stub.PrimaryMacAddress: got nil, want matcher-only stub")
	}
	if assigned.PrimaryMacAddress == mac {
		t.Errorf("cycle-break stub.PrimaryMacAddress aliased the rich MACAddress (would carry AssignedObject)")
	}
	if assigned.PrimaryMacAddress.AssignedObject != nil {
		t.Errorf("cycle-break stub.PrimaryMacAddress.AssignedObject must be nil; got %v", assigned.PrimaryMacAddress.AssignedObject)
	}
}

func TestTransformToVirtualMachine_DeepParentChain(t *testing.T) {
	// Depth-2 Parent chain reachable only via nested refs: A.Parent=B,
	// B.Parent=C. Only A is in the top-level slice. The harvest must
	// walk to fixed-point so both B and C land in the iface map.
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	c := &diode.Interface{Device: dev, Name: sp("eth-grandparent")}
	b := &diode.Interface{Device: dev, Name: sp("eth-parent"), Parent: c}
	a := &diode.Interface{Device: dev, Name: sp("eth-child"), Parent: b}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, a},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	names := map[string]*diode.VMInterface{}
	for _, e := range out {
		if v, ok := e.(*diode.VMInterface); ok && v.Name != nil {
			names[*v.Name] = v
		}
	}
	for _, want := range []string{"eth-child", "eth-parent", "eth-grandparent"} {
		if _, ok := names[want]; !ok {
			t.Errorf("expected VMInterface %q to be emitted via fixed-point harvest; got %v", want, names)
		}
	}
	if names["eth-child"].Parent != names["eth-parent"] {
		t.Errorf("child.Parent remap: got %p, want %p", names["eth-child"].Parent, names["eth-parent"])
	}
	if names["eth-parent"].Parent != names["eth-grandparent"] {
		t.Errorf("parent.Parent remap: got %p, want %p", names["eth-parent"].Parent, names["eth-grandparent"])
	}
}

func TestTransformToVirtualMachine_ParentBridgeRemapped(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	parent := &diode.Interface{Device: dev, Name: sp("eth0")}
	child := &diode.Interface{Device: dev, Name: sp("eth0.100"), Parent: parent}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, parent, child},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	var pVM, cVM *diode.VMInterface
	for _, e := range out {
		if v, ok := e.(*diode.VMInterface); ok {
			switch *v.Name {
			case "eth0":
				pVM = v
			case "eth0.100":
				cVM = v
			}
		}
	}
	if pVM == nil || cVM == nil {
		t.Fatalf("missing VMInterfaces: parent=%p child=%p", pVM, cVM)
	}
	if cVM.Parent != pVM {
		t.Errorf("Parent remap: got %p, want %p", cVM.Parent, pVM)
	}
}

func TestTransformToVirtualMachine_NoDeviceVLANOnlyNoOp(t *testing.T) {
	vlan := &diode.VLAN{Vid: ip64(100), Name: sp("users")}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{vlan},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	if len(out) != 1 || out[0] != vlan {
		t.Errorf("VLAN-only input should pass through unchanged: got %v", out)
	}
}

func TestTransformToVirtualMachine_OutputOrder(t *testing.T) {
	dev := &diode.Device{Name: sp("vyos-edge-1")}
	iface1 := &diode.Interface{Device: dev, Name: sp("eth0")}
	iface2 := &diode.Interface{Device: dev, Name: sp("eth1")}
	ip := &diode.IPAddress{Address: sp("192.0.2.1/24"), AssignedObject: iface1}
	vlan := &diode.VLAN{Vid: ip64(10)}
	out := mapping.TransformToVirtualMachine(
		[]diode.Entity{dev, iface1, iface2, ip, vlan},
		&config.Defaults{Type: config.TargetTypeVirtualMachine},
	)
	// Expected: VM, then VMInterfaces (top-level source order:
	// iface1, iface2), then IP, then VLAN.
	if len(out) != 5 {
		t.Fatalf("entity count: got %d, want 5", len(out))
	}
	if _, ok := out[0].(*diode.VirtualMachine); !ok {
		t.Errorf("out[0]: got %T, want *VirtualMachine", out[0])
	}
	for i := 1; i <= 2; i++ {
		if _, ok := out[i].(*diode.VMInterface); !ok {
			t.Errorf("out[%d]: got %T, want *VMInterface", i, out[i])
		}
	}
	if _, ok := out[3].(*diode.IPAddress); !ok {
		t.Errorf("out[3]: got %T, want *IPAddress", out[3])
	}
	if _, ok := out[4].(*diode.VLAN); !ok {
		t.Errorf("out[4]: got %T, want *VLAN", out[4])
	}
	vm0 := out[1].(*diode.VMInterface)
	vm1 := out[2].(*diode.VMInterface)
	if vm0.Name == nil || *vm0.Name != "eth0" {
		t.Errorf("out[1].Name: got %v, want eth0", vm0.Name)
	}
	if vm1.Name == nil || *vm1.Name != "eth1" {
		t.Errorf("out[2].Name: got %v, want eth1", vm1.Name)
	}
}
