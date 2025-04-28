package mapping

import (
	"fmt"

	"github.com/netboxlabs/diode-sdk-go/diode"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

// ObjectIDMapping is a map of ObjectIDs to entity types
type ObjectIDMapping map[string]string

// ObjectIDValueMap is a map of ObjectIDs to their values
type ObjectIDValueMap map[string]string

// ObjectIDMapper is a struct that maps ObjectIDs to entities
type ObjectIDMapper struct {
	mapping ObjectIDMapping
}

// NewObjectIDMapper creates a new ObjectIDMapper
func NewObjectIDMapper(mappings []config.MappingEntry) *ObjectIDMapper {
	mapping := make(ObjectIDMapping)
	for _, m := range mappings {
		mapping[m.OID] = fmt.Sprintf("%s.%s", m.Entity, m.Field)
	}
	return &ObjectIDMapper{
		mapping: mapping,
	}
}

// MapObjectIDsToEntity maps ObjectIDs to entities
// In future this will be dynamic based on the ObjectIDMapping from the policy
func (m *ObjectIDMapper) MapObjectIDsToEntity(objectIDs ObjectIDValueMap) []diode.Entity {
	for objectID, value := range objectIDs {
		entityType := m.mapping[objectID]
		switch entityType {
		case "ipAddress.address": // TODO: Add support for other entity types and move fields to within each case block
			ipEntity := &diode.IPAddress{
				Address: diode.String(value + "/32"),
			}

			return []diode.Entity{ipEntity}
		}
	}
	return []diode.Entity{}
}

// ObjectIDs returns the ObjectIDs that the ObjectIDMapper can map
func (m *ObjectIDMapper) ObjectIDs() []string {
	objectIDs := make([]string, 0, len(m.mapping))
	for objectID := range m.mapping {
		objectIDs = append(objectIDs, objectID)
	}
	return objectIDs
}
