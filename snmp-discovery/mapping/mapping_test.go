package mapping_test

import (
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/stretchr/testify/assert"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/mapping"
)

func TestMapObjectIDsToEntity(t *testing.T) {
	mapper := mapping.NewObjectIDMapper([]config.MappingEntry{
		{
			OID:    "1.3.6.1.2.1.4.20.1.1",
			Entity: "ipAddress",
			Field:  "address",
		},
	})
	objectIDs := mapping.ObjectIDValueMap{
		"1.3.6.1.2.1.4.20.1.1": "192.168.1.1",
	}

	entities := mapper.MapObjectIDsToEntity(objectIDs)

	assert.Len(t, entities, 1)
	ipEntity, ok := entities[0].(*diode.IPAddress)
	assert.True(t, ok)
	assert.Equal(t, "192.168.1.1/32", *ipEntity.Address)
}

func TestObjectIDs(t *testing.T) {
	mapper := mapping.NewObjectIDMapper([]config.MappingEntry{
		{
			OID:    "1.3.6.1.2.1.4.20.1.1",
			Entity: "ipAddress",
			Field:  "address",
		},
	})

	expectedObjectIDs := []string{
		"1.3.6.1.2.1.4.20.1.1",
	}

	objectIDs := mapper.ObjectIDs()

	assert.ElementsMatch(t, expectedObjectIDs, objectIDs)
}
