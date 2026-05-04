package qbridge

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// loadFixture reads a YAML synthetic-walk fixture from testdata/.
//
// Schema: top-level map of OID strings to {value, type}. Hand-authored
// to keep v1 self-contained without a snmpwalk-derived capture step.
func loadFixture(t *testing.T, name string) map[string]struct {
	Value string `yaml:"value"`
	Type  string `yaml:"type"`
} {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	out := map[string]struct {
		Value string `yaml:"value"`
		Type  string `yaml:"type"`
	}{}
	if err := yaml.Unmarshal(body, &out); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	return out
}

func TestFixture_AristaEOS_LoadsAndContainsExpectedOIDs(t *testing.T) {
	rows := loadFixture(t, "arista_eos.yaml")
	for _, oid := range []string{
		".1.3.6.1.2.1.1.2.0",                 // sysObjectID
		".1.3.6.1.2.1.17.1.4.1.2.1",          // dot1dBasePortIfIndex
		".1.3.6.1.2.1.17.7.1.4.3.1.1.10",     // dot1qVlanStaticName for VID 10
	} {
		if _, ok := rows[oid]; !ok {
			t.Errorf("fixture missing %s", oid)
		}
	}
}

func TestFixture_CiscoIOSXE_LoadsAndHasOverlayOIDs(t *testing.T) {
	rows := loadFixture(t, "cisco_iosxe.yaml")
	for _, oid := range []string{
		".1.3.6.1.2.1.1.2.0",                  // sysObjectID (Cisco)
		".1.3.6.1.4.1.9.9.68.1.5.1.1.1",       // vmVoiceVlanId for ifIndex 1
		".1.3.6.1.2.1.17.7.1.4.3.1.1.20",      // dot1qVlanStaticName for VID 20
	} {
		if _, ok := rows[oid]; !ok {
			t.Errorf("fixture missing %s", oid)
		}
	}
}
