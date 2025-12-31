package mapping_test

import (
	"log/slog"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/mapping"
	"github.com/stretchr/testify/assert"
)

func TestMapObjectIDsToEntity_WithStackMembers(t *testing.T) {
	logger := slog.Default()

	mappingConfig := mapping.NewConfig([]config.MappingEntry{
		{
			OID:    ".1.3.6.1.2.1.1",
			Entity: "device",
			Field:  "_id",
			MappingEntries: []config.MappingEntry{
				{
					OID:    ".1.3.6.1.2.1.1.5.0",
					Entity: "device",
					Field:  "name",
				},
			},
		},
		{
			OID:            ".1.3.6.1.2.1.2.2.1",
			Entity:         "interface",
			Field:          "_id",
			IdentifierSize: 1,
			MappingEntries: []config.MappingEntry{
				{
					OID:    ".1.3.6.1.2.1.2.2.1.2",
					Entity: "interface",
					Field:  "name",
				},
			},
		},
	}, logger, &FakeManufacturers{}, &FakeDeviceLookup{})

	objectIDs := mapping.ObjectIDValueMap{
		".1.3.6.1.2.1.1.5.0": mapping.Value{Value: "stack-01", Type: mapping.Asn1BER(mapping.OctetString)},

		".1.3.6.1.2.1.2.2.1.2.101": mapping.Value{Value: "Gi1/0/1", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},
		".1.3.6.1.2.1.2.2.1.2.201": mapping.Value{Value: "Gi2/0/1", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},

		".1.3.6.1.2.1.47.1.1.1.1.4.1":  mapping.Value{Value: "0", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.4.2":  mapping.Value{Value: "0", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":  mapping.Value{Value: "3", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.5.2":  mapping.Value{Value: "3", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.6.1":  mapping.Value{Value: "1", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.6.2":  mapping.Value{Value: "2", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.7.1":  mapping.Value{Value: "Switch 1", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.7.2":  mapping.Value{Value: "Switch 2", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.11.1": mapping.Value{Value: "SERIAL1", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.11.2": mapping.Value{Value: "SERIAL2", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},

		".1.3.6.1.2.1.47.1.3.2.1.2.1.1.3.6.1.2.1.2.2.1.1.101": mapping.Value{Value: "101", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.3.2.1.2.2.1.3.6.1.2.1.2.2.1.1.201": mapping.Value{Value: "201", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
	}

	mapper := mapping.NewObjectIDMapper(mappingConfig, logger, &config.Defaults{})
	entities := mapper.MapObjectIDsToEntity(objectIDs)

	var virtualChassis *diode.VirtualChassis
	devices := make([]*diode.Device, 0)
	interfaces := make([]*diode.Interface, 0)

	for _, entity := range entities {
		switch e := entity.(type) {
		case *diode.VirtualChassis:
			virtualChassis = e
		case *diode.Device:
			devices = append(devices, e)
		case *diode.Interface:
			interfaces = append(interfaces, e)
		}
	}

	assert.NotNil(t, virtualChassis)
	assert.Len(t, devices, 2)
	assert.Len(t, interfaces, 2)

	deviceByName := make(map[string]*diode.Device)
	for _, device := range devices {
		if device.Name != nil {
			deviceByName[*device.Name] = device
		}
		assert.NotNil(t, device.VirtualChassis)
		assert.NotNil(t, device.VcPosition)
	}

	assert.Contains(t, deviceByName, "Switch 1")
	assert.Contains(t, deviceByName, "Switch 2")
	assert.Equal(t, int64(1), *deviceByName["Switch 1"].VcPosition)
	assert.Equal(t, "SERIAL1", derefString(deviceByName["Switch 1"].Serial))
	assert.Equal(t, int64(2), *deviceByName["Switch 2"].VcPosition)
	assert.Equal(t, "SERIAL2", derefString(deviceByName["Switch 2"].Serial))

	assert.NotNil(t, virtualChassis.Master)
	assert.Equal(t, "Switch 1", derefString(virtualChassis.Master.Name))

	interfaceDevices := make(map[string]string)
	for _, iface := range interfaces {
		if iface.Name == nil || iface.Device == nil || iface.Device.Name == nil {
			continue
		}
		interfaceDevices[*iface.Name] = *iface.Device.Name
	}
	assert.Equal(t, "Switch 1", interfaceDevices["Gi1/0/1"])
	assert.Equal(t, "Switch 2", interfaceDevices["Gi2/0/1"])
}

func TestMapObjectIDsToEntity_WithModules(t *testing.T) {
	logger := slog.Default()

	mappingConfig := mapping.NewConfig([]config.MappingEntry{
		{
			OID:    ".1.3.6.1.2.1.1",
			Entity: "device",
			Field:  "_id",
			MappingEntries: []config.MappingEntry{
				{
					OID:    ".1.3.6.1.2.1.1.5.0",
					Entity: "device",
					Field:  "name",
				},
			},
		},
	}, logger, &FakeManufacturers{}, &FakeDeviceLookup{})

	objectIDs := mapping.ObjectIDValueMap{
		".1.3.6.1.2.1.1.5.0": mapping.Value{Value: "switch-01", Type: mapping.Asn1BER(mapping.OctetString)},

		".1.3.6.1.2.1.47.1.1.1.1.4.1": mapping.Value{Value: "0", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.4.2": mapping.Value{Value: "1", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.4.3": mapping.Value{Value: "2", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},

		".1.3.6.1.2.1.47.1.1.1.1.5.1": mapping.Value{Value: "3", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.5.2": mapping.Value{Value: "5", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.5.3": mapping.Value{Value: "9", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},

		".1.3.6.1.2.1.47.1.1.1.1.6.2": mapping.Value{Value: "1", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.6.3": mapping.Value{Value: "1", Type: mapping.Asn1BER(mapping.Integer), IdentifierSize: 1},

		".1.3.6.1.2.1.47.1.1.1.1.7.2":  mapping.Value{Value: "Slot 1", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.7.3":  mapping.Value{Value: "Uplink Module 1", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.11.3": mapping.Value{Value: "MOD123", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.12.3": mapping.Value{Value: "Cisco", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},
		".1.3.6.1.2.1.47.1.1.1.1.13.3": mapping.Value{Value: "C9300-NM-8X", Type: mapping.Asn1BER(mapping.OctetString), IdentifierSize: 1},
	}

	mapper := mapping.NewObjectIDMapper(mappingConfig, logger, &config.Defaults{})
	entities := mapper.MapObjectIDsToEntity(objectIDs)

	var device *diode.Device
	var module *diode.Module
	var moduleBay *diode.ModuleBay
	var moduleType *diode.ModuleType

	for _, entity := range entities {
		switch e := entity.(type) {
		case *diode.Device:
			device = e
		case *diode.Module:
			module = e
		case *diode.ModuleBay:
			moduleBay = e
		case *diode.ModuleType:
			moduleType = e
		}
	}

	assert.NotNil(t, device)
	assert.NotNil(t, module)
	assert.NotNil(t, moduleBay)
	assert.NotNil(t, moduleType)

	assert.Equal(t, "Slot 1", derefString(moduleBay.Name))
	assert.Equal(t, "MOD123", derefString(module.Serial))
	assert.NotNil(t, module.ModuleBay)
	assert.NotNil(t, module.ModuleType)
	assert.Equal(t, "C9300-NM-8X", derefString(module.ModuleType.Model))
	assert.Equal(t, "Cisco", derefString(module.ModuleType.Manufacturer.Name))
	assert.Equal(t, device, module.Device)
	assert.Equal(t, device, moduleBay.Device)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
