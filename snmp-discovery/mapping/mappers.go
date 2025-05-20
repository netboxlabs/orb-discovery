package mapping

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

// IPAddressMapper is a struct that maps IP addresses to entities
type IPAddressMapper struct{}

// Map maps IP addresses to entities
func (m *IPAddressMapper) Map(values map[ObjectIDIndex]*ObjectIDValue, mappingEntry *mappingEntry, entityRegistry *EntityRegistry, logger *slog.Logger) diode.Entity {
	logger.Debug("Mapping values to ipAddress entity", "values", values, "mappingEntry", mappingEntry)
	ipAddress := diode.IPAddress{}

	// for each value in the map, map it to the ip address entity
	for objectID, value := range values {
		logger.Debug("Mapping value to ipAddress entity", "objectID", objectID, "value", value)
		for _, propertyMappingEntry := range mappingEntry.MappingEntries {
			if objectID.HasParent(mappingEntry.OID) {
				switch propertyMappingEntry.Field {
				case "address":
					x := fmt.Sprintf("%s/32", string(value.Index))
					ipAddress.Address = &x
				case "assigned_object":
					if propertyMappingEntry.Relationship != (config.Relationship{}) {
						linkedEntity := entityRegistry.GetOrCreateEntity(EntityType(propertyMappingEntry.Relationship.Type), ObjectIDIndex(value.Value))
						if linkedEntity == nil {
							logger.Warn("No linked entity found while mapping assigned object", "relationship", propertyMappingEntry.Relationship)
							continue
						}
						// Handle relationship mapping
						if propertyMappingEntry.Relationship.Type == "interface" {
							ipAddress.AssignedObject = linkedEntity.(*diode.Interface)
						}
					}
				default:
					logger.Warn("Unknown field", "field", mappingEntry.Field)
				}
			}
		}
	}
	return &ipAddress
}

// InterfaceMapper is a struct that maps interfaces to entities
type InterfaceMapper struct{}

// Map maps interfaces to entities
func (m *InterfaceMapper) Map(values map[ObjectIDIndex]*ObjectIDValue, mappingEntry *mappingEntry, entityRegistry *EntityRegistry, logger *slog.Logger) diode.Entity {
	logger.Debug("Mapping values to interface entity", "values", values, "mappingEntry", mappingEntry)
	interfaceEntity := entityRegistry.GetOrCreateEntity(EntityType(mappingEntry.Entity), getIndex(values)).(*diode.Interface)

	for objectID, value := range values {
		for _, propertyMappingEntry := range mappingEntry.MappingEntries {
			if objectID.HasParent(propertyMappingEntry.OID) {
				logger.Debug("Mapping value to interface entity with mapper", "objectID", objectID, "value", value, "mappingEntry", propertyMappingEntry)
				switch propertyMappingEntry.Field {
				case "name":
					interfaceEntity.Name = &value.Value
				case "speed":
					speed, err := strconv.Atoi(value.Value)
					if err != nil {
						logger.Warn("Error converting speed to int", "error", err, "value", value.Value)
						continue
					}
					speed64 := int64(speed)
					interfaceEntity.Speed = &speed64
				case "macAddress":
					interfaceEntity.PrimaryMacAddress = &diode.MACAddress{
						MacAddress: &value.Value,
					}
				case "adminStatus":
					enabled := value.Value == "1"
					interfaceEntity.Enabled = &enabled
				default:
					logger.Warn("Unknown field", "field", propertyMappingEntry.Field)
				}
			}
		}
	}
	return interfaceEntity
}

// DeviceMapper is a struct that maps devices to entities
type DeviceMapper struct{}

// Map maps devices to entities
func (m *DeviceMapper) Map(values map[ObjectIDIndex]*ObjectIDValue, mappingEntry *mappingEntry, entityRegistry *EntityRegistry, logger *slog.Logger) diode.Entity {
	logger.Debug("Mapping values to device entity", "values", values, "mappingEntry", mappingEntry)
	device := diode.Device{}

	for objectID, value := range values {
		for _, propertyMappingEntry := range mappingEntry.MappingEntries {
			if objectID.HasParent(propertyMappingEntry.OID) {
				logger.Debug("Mapping value to device entity with mapper", "objectID", objectID, "value", value, "mappingEntry", propertyMappingEntry)
				switch propertyMappingEntry.Field {
				case "name":
					device.Name = &value.Value
				default:
					logger.Warn("Unknown field", "field", propertyMappingEntry.Field)
				}
			}
		}
	}
	return &device
}
