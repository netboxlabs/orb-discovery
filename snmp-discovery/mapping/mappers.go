package mapping

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/data"
)

// IPAddressMapper is a struct that maps IP addresses to entities
type IPAddressMapper struct{}

// Map maps IP addresses to entities
func (m *IPAddressMapper) Map(values map[ObjectIDIndex]*ObjectIDValue, mappingEntry *Entry, entityRegistry *EntityRegistry, logger *slog.Logger) diode.Entity {
	logger.Debug("Mapping values to ipAddress entity", "values", values, "mappingEntry", mappingEntry)
	ipAddress := diode.IPAddress{}

	fieldFound := false
	// for each value in the map, map it to the ip address entity
	for objectID, value := range values {
		logger.Debug("Mapping value to ipAddress entity", "objectID", objectID, "value", value)
		for _, propertyMappingEntry := range mappingEntry.MappingEntries {
			if objectID.HasParent(mappingEntry.OID) {
				switch propertyMappingEntry.Field {
				case "address":
					x := fmt.Sprintf("%s/32", string(value.Index))
					ipAddress.Address = &x
					fieldFound = true
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
							fieldFound = true
						}
					}
				default:
					logger.Warn("Unknown field", "field", mappingEntry.Field)
				}
			}
		}
	}

	if fieldFound {
		applyEntityDefaults(&ipAddress, entityRegistry.GetDefaults(), func(defaults config.Defaults) config.EntityDefaults {
			return defaults.IPAddress
		})
	}

	return &ipAddress
}

// InterfaceMapper is a struct that maps interfaces to entities
type InterfaceMapper struct{}

// applyEntityDefaults applies default values to an entity based on the provided defaults
func applyEntityDefaults(entity interface{}, defaults *config.Defaults, getEntityDefaults func(defaults config.Defaults) config.EntityDefaults) {
	if defaults == nil {
		return
	}
	entityDefaults := getEntityDefaults(*defaults)

	// Apply entity-specific defaults
	if entityDefaults.Description != "" {
		switch e := entity.(type) {
		case *diode.Interface:
			e.Description = &entityDefaults.Description
		case *diode.Device:
			e.Description = &entityDefaults.Description
		case *diode.IPAddress:
			e.Description = &entityDefaults.Description
		}
	}

	// Collect tags from both entity-specific and global defaults
	var tags []*diode.Tag
	if len(entityDefaults.Tags) > 0 {
		for _, tag := range entityDefaults.Tags {
			tags = append(tags, &diode.Tag{Name: &tag})
		}
	}
	if len(defaults.Tags) > 0 {
		for _, tag := range defaults.Tags {
			tags = append(tags, &diode.Tag{Name: &tag})
		}
	}

	// Apply tags if any exist
	if len(tags) > 0 {
		switch e := entity.(type) {
		case *diode.Interface:
			e.Tags = tags
		case *diode.Device:
			e.Tags = tags
		case *diode.IPAddress:
			e.Tags = tags
		}
	}

	// Apply global defaults if not overridden by entity-specific defaults
	switch e := entity.(type) {
	case *diode.Interface:
		if e.Description == nil && defaults.Description != "" {
			e.Description = &defaults.Description
		}
	case *diode.Device:
		if e.Description == nil && defaults.Description != "" {
			e.Description = &defaults.Description
		}
	case *diode.IPAddress:
		if e.Description == nil && defaults.Description != "" {
			e.Description = &defaults.Description
		}
		if e.Comments == nil && defaults.Comments != "" {
			e.Comments = &defaults.Comments
		}
	}
}

// Map maps interfaces to entities
func (m *InterfaceMapper) Map(values map[ObjectIDIndex]*ObjectIDValue, mappingEntry *Entry, entityRegistry *EntityRegistry, logger *slog.Logger) diode.Entity {
	logger.Debug("Mapping values to interface entity", "values", values, "mappingEntry", mappingEntry)
	interfaceEntity := entityRegistry.GetOrCreateEntity(EntityType(mappingEntry.Entity), getIndex(values)).(*diode.Interface)

	fieldFound := false
	for objectID, value := range values {
		for _, propertyMappingEntry := range mappingEntry.MappingEntries {
			if objectID.HasParent(propertyMappingEntry.OID) {
				logger.Debug("Mapping value to interface entity with mapper", "objectID", objectID, "value", value, "mappingEntry", propertyMappingEntry)
				switch propertyMappingEntry.Field {
				case "name":
					interfaceEntity.Name = &value.Value
					fieldFound = true
				case "speed":
					speed, err := strconv.Atoi(value.Value)
					if err != nil {
						logger.Warn("Error converting speed to int", "error", err, "value", value.Value)
						continue
					}
					speed64 := int64(speed)
					interfaceEntity.Speed = &speed64
					fieldFound = true
				case "macAddress":
					interfaceEntity.PrimaryMacAddress = &diode.MACAddress{
						MacAddress: &value.Value,
					}
					fieldFound = true
				case "adminStatus":
					enabled := value.Value == "1"
					interfaceEntity.Enabled = &enabled
					fieldFound = true
				default:
					logger.Warn("Unknown field", "field", propertyMappingEntry.Field)
				}
			}
		}
	}

	// Apply defaults if available
	if fieldFound {
		applyEntityDefaults(interfaceEntity, entityRegistry.GetDefaults(), func(defaults config.Defaults) config.EntityDefaults {
			return defaults.Interface
		})
	}

	return interfaceEntity
}

// DeviceMapper is a struct that maps devices to entities
type DeviceMapper struct {
	devices data.DeviceDataRetreiver
}

// NewDeviceMapper creates a new DeviceMapper
func NewDeviceMapper(devices data.DeviceDataRetreiver) *DeviceMapper {
	return &DeviceMapper{
		devices: devices,
	}
}

// Map maps devices to entities
func (m *DeviceMapper) Map(values map[ObjectIDIndex]*ObjectIDValue, mappingEntry *Entry, entityRegistry *EntityRegistry, logger *slog.Logger) diode.Entity {
	logger.Debug("Mapping values to device entity", "values", values, "mappingEntry", mappingEntry)
	deviceEntity := entityRegistry.GetOrCreateEntity(EntityType(mappingEntry.Entity), getIndex(values)).(*diode.Device)

	fieldFound := false
	for objectID, value := range values {
		for _, propertyMappingEntry := range mappingEntry.MappingEntries {
			if objectID.HasParent(propertyMappingEntry.OID) {
				logger.Debug("Mapping value to device entity with mapper", "objectID", objectID, "value", value, "mappingEntry", propertyMappingEntry)
				switch propertyMappingEntry.Field {
				case "name":
					deviceEntity.Name = &value.Value
					fieldFound = true
				case "platform":
					// Use getDeviceIDs to get the manufacturer and model
					manufacturerID, modelID, err := m.getDeviceIDs(value.Value)
					if err != nil {
						logger.Warn("Error getting device IDs", "error", err, "value", value.Value)
						continue
					}
					manufacturer, err := m.devices.GetManufacturer(manufacturerID)
					if err != nil {
						logger.Warn("Error getting manufacturer", "error", err, "manufacturerID", manufacturerID)
						continue
					}

					manufacturerEntity := diode.Manufacturer{
						Name: &manufacturer,
					}

					deviceEntity.Platform = &diode.Platform{
						Manufacturer: &manufacturerEntity,
					}

					deviceModel, err := m.devices.GetDeviceModel(modelID)
					if err != nil {
						logger.Warn("Error getting device model", "error", err, "modelID", modelID)
					}
					deviceEntity.DeviceType = &diode.DeviceType{
						Model:        &deviceModel,
						Manufacturer: &manufacturerEntity,
					}
					fieldFound = true
				default:
					logger.Warn("Unknown field", "field", propertyMappingEntry.Field)
				}
			}
		}
	}

	// Apply defaults if available
	if fieldFound {
		applyEntityDefaults(deviceEntity, entityRegistry.GetDefaults(), func(defaults config.Defaults) config.EntityDefaults {
			return defaults.Device
		})
	}

	return deviceEntity
}

func (m *DeviceMapper) getDeviceIDs(objectID string) (int, int, error) {
	parts := strings.Split(objectID, ".")
	if len(parts) > 0 && parts[0] == "" {
		parts = parts[1:]
	}

	const ManufacturerIDIndex = 6
	// Check if we have enough parts to extract manufacturer and model IDs
	if len(parts) > ManufacturerIDIndex {
		manID, err := strconv.Atoi(parts[ManufacturerIDIndex])
		if err != nil {
			return 0, 0, err
		}

		modelID, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			return 0, 0, err
		}

		return manID, modelID, nil
	}

	return 0, 0, fmt.Errorf("invalid objectID: %s", objectID)
}
