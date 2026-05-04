package mapping

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"gopkg.in/yaml.v3"
)

// loadFixtureAsObjectIDValueMap reads a qbridge testdata fixture and
// translates it to the runtime ObjectIDValueMap shape VlanMapper consumes.
func loadFixtureAsObjectIDValueMap(t *testing.T, relPath string) ObjectIDValueMap {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("qbridge", relPath))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var raw map[string]struct {
		Value string `yaml:"value"`
		Type  string `yaml:"type"`
	}
	if err := yaml.Unmarshal(body, &raw); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	out := ObjectIDValueMap{}
	for oid, v := range raw {
		out[oid] = Value{Value: v.Value, Type: asn1berFromName(v.Type)}
	}
	return out
}

func asn1berFromName(n string) Asn1BER {
	switch n {
	case "Integer":
		return Integer
	case "OctetString":
		return OctetString
	case "ObjectIdentifier":
		return ObjectIdentifier
	}
	return UnknownType
}

func TestVlanMapper_E2E_AristaEOS(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	rows := loadFixtureAsObjectIDValueMap(t, "testdata/arista_eos.yaml")

	registry := NewEntityRegistry(logger)
	iface := &diode.Interface{Name: StringPtr("Ethernet1")}
	if registry.entities[InterfaceEntityType] == nil {
		registry.entities[InterfaceEntityType] = map[ObjectIDIndex]diode.Entity{}
	}
	registry.entities[InterfaceEntityType]["1"] = iface
	registry.MarkInterfaceVerified(iface)

	vm := NewVlanMapper(logger)
	emitted := vm.PostMap(rows, registry, &config.Defaults{})

	if iface.Mode == nil || *iface.Mode != "access" {
		t.Errorf("Mode: got %v, want access", iface.Mode)
	}
	if iface.UntaggedVlan == nil || iface.UntaggedVlan.Vid == nil || *iface.UntaggedVlan.Vid != 10 {
		t.Errorf("UntaggedVlan.Vid: got %+v, want 10", iface.UntaggedVlan)
	}
	if len(emitted) != 1 {
		t.Errorf("emitted: got %d entities, want 1 (one VLAN)", len(emitted))
	}
}

func TestVlanMapper_E2E_CiscoIOSXE_VoicePromotion(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	rows := loadFixtureAsObjectIDValueMap(t, "testdata/cisco_iosxe.yaml")

	registry := NewEntityRegistry(logger)
	iface := &diode.Interface{Name: StringPtr("Gig1/0/1")}
	if registry.entities[InterfaceEntityType] == nil {
		registry.entities[InterfaceEntityType] = map[ObjectIDIndex]diode.Entity{}
	}
	registry.entities[InterfaceEntityType]["1"] = iface
	registry.MarkInterfaceVerified(iface)

	vm := NewVlanMapper(logger)
	emitted := vm.PostMap(rows, registry, &config.Defaults{})

	if iface.Mode == nil || *iface.Mode != "tagged" {
		t.Errorf("Mode: got %v, want tagged (voice promotion)", iface.Mode)
	}
	if iface.UntaggedVlan == nil || iface.UntaggedVlan.Vid == nil || *iface.UntaggedVlan.Vid != 10 {
		t.Errorf("UntaggedVlan.Vid: got %+v, want 10", iface.UntaggedVlan)
	}
	if len(iface.TaggedVlans) != 1 || iface.TaggedVlans[0].Vid == nil || *iface.TaggedVlans[0].Vid != 20 {
		t.Errorf("TaggedVlans: got %+v, want [{Vid:20}]", iface.TaggedVlans)
	}
	vlanCount := 0
	for _, e := range emitted {
		if _, ok := e.(*diode.VLAN); ok {
			vlanCount++
		}
	}
	if vlanCount != 2 {
		t.Errorf("emitted VLAN count: got %d, want 2 (data + voice)", vlanCount)
	}
}
