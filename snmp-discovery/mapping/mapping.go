package mapping

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

// ObjectIDValueMap is a map of ObjectIDs to their values
type ObjectIDValueMap map[string]string

// ObjectIDMapper is a struct that maps ObjectIDs to entities
type ObjectIDMapper struct {
	mapping map[string]*mappingEntry
	logger  *slog.Logger
}

type mappingEntry struct {
	OID            string
	Entity         string
	Field          string
	MappingEntries []mappingEntry
	Mapper         orbToEntityMapper
}

var entityMappers = map[string]orbToEntityMapper{
	"ipAddress": &ipAddressMapper{},
	"interface": &interfaceMapper{},
}

func (m *mappingEntry) MapToEntity(object map[string]string, logger *slog.Logger) []diode.Entity {
	logger.Debug("Mapping value to entity", "value", object)
	if m.Mapper == nil {
		logger.Warn("No mapper found for entity. Ignoring.", "entity", m.Entity)
		return nil
	}
	entity := m.Mapper.Map(object, m, logger)
	logger.Debug("Entity returned from mapper", "entity", entity)
	if entity == nil {
		logger.Warn("No entity returned from mapper. Ignoring.", "entity", m.Entity)
		return nil
	}
	return []diode.Entity{entity}
}

// NewObjectIDMapper creates a new ObjectIDMapper
func NewObjectIDMapper(mappings []config.MappingEntry, logger *slog.Logger) *ObjectIDMapper {
	mapping := make(map[string]*mappingEntry)
	for _, m := range mappings {
		logger.Debug("Adding mapping", "oid", m.OID, "entity", m.Entity, "field", m.Field)
		mappingEntry := newMappingEntry(m, logger)
		if mappingEntry == nil {
			continue
		}
		mapping[m.OID] = mappingEntry
		logger.Debug("Mapping entry added", "oid", mappingEntry.OID, "entity", mappingEntry.Entity, "field", mappingEntry.Field)
	}
	return &ObjectIDMapper{
		mapping: mapping,
		logger:  logger,
	}
}

type orbToEntityMapper interface {
	Map(values map[string]string, mappingEntry *mappingEntry, logger *slog.Logger) diode.Entity
}

type ipAddressMapper struct{}

func (m *ipAddressMapper) Map(values map[string]string, mappingEntry *mappingEntry, _ *slog.Logger) diode.Entity {
	ipAddress := diode.IPAddress{}

	// for each value in the map, map it to the ip address entity
	for objectID, value := range values {
		if objectID == mappingEntry.OID {
			switch mappingEntry.Field {
			case "address":
				ipAddress.Address = &value
			}
		}
	}
	return &ipAddress
}

type interfaceMapper struct{}

func (m *interfaceMapper) Map(values map[string]string, mappingEntry *mappingEntry, logger *slog.Logger) diode.Entity {
	logger.Debug("Mapping values to interface entity", "values", values, "mappingEntry", mappingEntry)
	interfaceEntity := diode.Interface{}
	// for each value in the map, map it to the interface entity
	for objectID, value := range values {
		logger.Debug("Mapping value to interface entity", "objectID", objectID, "value", value, "mappingEntry", mappingEntry)
		for _, childMappingEntry := range mappingEntry.MappingEntries {
			if objectID == childMappingEntry.OID {
				switch childMappingEntry.Field {
				case "name":
					interfaceEntity.Name = &value
				case "speed":
					speed, err := strconv.Atoi(value)
					if err != nil {
						panic(err)
					}
					speed32 := int32(speed)
					interfaceEntity.Speed = &speed32
				case "macAddress":
					interfaceEntity.MacAddress = &value
				}
			}
		}
	}
	return &interfaceEntity
}

func newMappingEntry(m config.MappingEntry, logger *slog.Logger) *mappingEntry {
	mapper := entityMappers[m.Entity]
	if mapper == nil {
		logger.Warn("No mapper found for entity. Ignoring.", "entity", m.Entity)
		return nil
	}
	return &mappingEntry{
		OID:            m.OID,
		Entity:         m.Entity,
		Field:          m.Field,
		Mapper:         mapper,
		MappingEntries: newChildMappingEntries(m.MappingEntries, logger),
	}
}

func newChildMappingEntries(configMappingEntries []config.MappingEntry, logger *slog.Logger) []mappingEntry {
	childMappingEntries := make([]mappingEntry, 0, len(configMappingEntries))
	for _, m := range configMappingEntries {
		child := &mappingEntry{
			OID:            m.OID,
			Entity:         m.Entity,
			Field:          m.Field,
			MappingEntries: newChildMappingEntries(m.MappingEntries, logger),
		}
		childMappingEntries = append(childMappingEntries, *child)
	}
	return childMappingEntries
}

// ObjectIDIndex is a struct that contains an index and a map of values
type ObjectIDIndex struct {
	Index  string
	Values map[string]string
}

// NewObjectIDIndex creates a new ObjectIDIndex
func NewObjectIDIndex(index string) *ObjectIDIndex {
	return &ObjectIDIndex{
		Index:  index,
		Values: make(map[string]string),
	}
}

// MapObjectIDsToEntity maps ObjectIDs to entities
func (m *ObjectIDMapper) MapObjectIDsToEntity(objectIDs ObjectIDValueMap) []diode.Entity {
	objectIDIndexMap := m.groupByObjectIDIndex(objectIDs)

	m.logger.Debug("ObjectIDIndexMap", "objectIDIndexMap", objectIDIndexMap)

	entities := make([]diode.Entity, 0, len(objectIDIndexMap))
	for _, value := range objectIDIndexMap {
		mappingEntry, err := m.getMappingEntry(value.Index, m.logger)
		if err != nil {
			m.logger.Warn("Error finding mapping entry", "error", err, "objectID", value.Index)
			continue
		}
		entities = append(entities, mappingEntry.MapToEntity(value.Values, m.logger)...)
	}
	return entities
}

func (m *ObjectIDMapper) groupByObjectIDIndex(objectIDs ObjectIDValueMap) map[string]*ObjectIDIndex {
	objectIDIndexMap := make(map[string]*ObjectIDIndex)
	for objectID, value := range objectIDs {
		parts := strings.Split(objectID, ".")
		id := parts[len(parts)-1]
		if objectIDIndexMap[id] == nil {
			objectIDIndexMap[id] = NewObjectIDIndex(strings.Join(parts[:len(parts)-1], "."))
		}
		objectIDIndexMap[id].Values[strings.Join(parts[:len(parts)-1], ".")] = value
	}
	return objectIDIndexMap
}

// Gets the mapper for the closest parent objectID
func (m *ObjectIDMapper) getMappingEntry(objectID string, logger *slog.Logger) (*mappingEntry, error) {
	mappingKeys := make([]string, 0, len(m.mapping))
	for k := range m.mapping {
		mappingKeys = append(mappingKeys, k)
	}
	logger.Debug("Getting mapping entry for objectID", "objectID", objectID, "mappingKeys", mappingKeys)

	for {
		if value, found := m.mapping[objectID]; found {
			return value, nil
		}
		// Split the key by the last '.'
		lastDotIndex := strings.LastIndex(objectID, ".")
		if lastDotIndex == -1 {
			break
		}
		objectID = objectID[:lastDotIndex]
	}
	return nil, fmt.Errorf("no mapping entry found for objectID %s", objectID)
}

// ObjectIDs returns the ObjectIDs that the ObjectIDMapper can map
func (m *ObjectIDMapper) ObjectIDs() []string {
	objectIDs := make([]string, 0, len(m.mapping))
	for objectID := range m.mapping {
		objectIDs = append(objectIDs, objectID)
	}
	return objectIDs
}
