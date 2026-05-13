package mapping

import (
	"log/slog"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

// strPtr is declared in stubs_test.go (same package mapping), so it
// is reachable from here without redeclaration.

func newTestDeviceMapper() *DeviceMapper {
	return &DeviceMapper{logger: slog.Default()}
}

func TestDeviceMapper_applyDefaults_LocationLiteral(t *testing.T) {
	m := newTestDeviceMapper()
	entity := &diode.Device{}
	defaults := &config.Defaults{Location: "DC1-Building-A", Site: "dc1"}
	m.applyDefaults(entity, defaults, nil)
	require.NotNil(t, entity.Location)
	require.NotNil(t, entity.Location.Name)
	assert.Equal(t, "DC1-Building-A", *entity.Location.Name)
	require.NotNil(t, entity.Location.Site)
	require.NotNil(t, entity.Location.Site.Name)
	assert.Equal(t, "dc1", *entity.Location.Site.Name)
}

func TestDeviceMapper_applyDefaults_LocationNonSNMPDecimalIsLiteral(t *testing.T) {
	// "3.14.159" is a literal (room number), not an OID — must not be
	// re-classified even if a walked map is present.
	m := newTestDeviceMapper()
	entity := &diode.Device{}
	defaults := &config.Defaults{Location: "3.14.159", Site: "dc1"}
	walked := map[string]string{".1.3.6.1.2.1.1.6.0": "Data Center 01"}
	m.applyDefaults(entity, defaults, walked)
	require.NotNil(t, entity.Location)
	require.NotNil(t, entity.Location.Name)
	assert.Equal(t, "3.14.159", *entity.Location.Name)
}

func TestDeviceMapper_applyDefaults_LocationOIDReferenceResolves(t *testing.T) {
	m := newTestDeviceMapper()
	entity := &diode.Device{}
	defaults := &config.Defaults{Location: ".1.3.6.1.2.1.1.6.0", Site: "dc1"}
	walked := map[string]string{".1.3.6.1.2.1.1.6.0": "Data Center 01"}
	m.applyDefaults(entity, defaults, walked)
	require.NotNil(t, entity.Location)
	require.NotNil(t, entity.Location.Name)
	assert.Equal(t, "Data Center 01", *entity.Location.Name)
	require.NotNil(t, entity.Location.Site)
	assert.Equal(t, "dc1", *entity.Location.Site.Name)
}

func TestDeviceMapper_applyDefaults_LocationOIDReferenceMissingSkips(t *testing.T) {
	m := newTestDeviceMapper()
	entity := &diode.Device{}
	defaults := &config.Defaults{Location: ".1.3.6.1.2.1.1.6.0", Site: "dc1"}
	m.applyDefaults(entity, defaults, map[string]string{})
	assert.Nil(t, entity.Location)
}

func TestDeviceMapper_applyDefaults_LocationOIDReferenceEmptySkips(t *testing.T) {
	m := newTestDeviceMapper()
	entity := &diode.Device{}
	defaults := &config.Defaults{Location: ".1.3.6.1.2.1.1.6.0", Site: "dc1"}
	walked := map[string]string{".1.3.6.1.2.1.1.6.0": "   "}
	m.applyDefaults(entity, defaults, walked)
	assert.Nil(t, entity.Location)
}

func TestDeviceMapper_applyDefaults_LocationOverridesPreSetValue(t *testing.T) {
	m := newTestDeviceMapper()
	existing := "pre-existing"
	entity := &diode.Device{Location: &diode.Location{Name: &existing}}
	defaults := &config.Defaults{Location: "DC1-Building-A", Site: "dc1"}
	m.applyDefaults(entity, defaults, nil)
	require.NotNil(t, entity.Location)
	require.NotNil(t, entity.Location.Name)
	assert.Equal(t, "DC1-Building-A", *entity.Location.Name)
}
