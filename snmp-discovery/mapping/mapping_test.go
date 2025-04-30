package mapping_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/stretchr/testify/assert"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/mapping"
)

func TestMapObjectIDsToEntity(t *testing.T) {
	tests := []struct {
		name      string
		mapping   []config.MappingEntry
		objectIDs mapping.ObjectIDValueMap
		expected  []diode.Entity
	}{
		{
			name: "Valid Mapping with multiple OIDs for same entity",
			mapping: []config.MappingEntry{
				{
					OID:    "iso.3.6.1.2.1.2.2.1",
					Entity: "interface",
					Field:  "_id",
					MappingEntries: []config.MappingEntry{
						{
							OID:    "iso.3.6.1.2.1.2.2.1.2",
							Entity: "interface",
							Field:  "name",
						},
						{
							OID:    "iso.3.6.1.2.1.2.2.1.5",
							Entity: "interface",
							Field:  "speed",
						},
						{
							OID:    "iso.3.6.1.2.1.2.2.1.6",
							Entity: "interface",
							Field:  "macAddress",
						},
						{
							OID:    "iso.3.6.1.2.1.2.2.1.7",
							Entity: "interface",
							Field:  "adminStatus",
						},
					},
				},
			},
			objectIDs: mapping.ObjectIDValueMap{
				"iso.3.6.1.2.1.2.2.1.2.999": "GigabitEthernet1/0/1",
				"iso.3.6.1.2.1.2.2.1.5.999": "1000000000",
				"iso.3.6.1.2.1.2.2.1.6.999": "00:00:00:00:00:00",
				"iso.3.6.1.2.1.2.2.1.7.999": "1",
			},
			expected: []diode.Entity{
				&diode.Interface{
					Speed:      &[]int32{1000000000}[0],
					Name:       diode.String("GigabitEthernet1/0/1"),
					MacAddress: &[]string{"00:00:00:00:00:00"}[0],
					Enabled:    &[]bool{true}[0],
				},
			},
		},
		{
			name: "Valid Mapping for multiple entities of same type",
			mapping: []config.MappingEntry{
				{
					OID:    "iso.3.6.1.2.1.2.2.1",
					Entity: "interface",
					Field:  "_id",
					MappingEntries: []config.MappingEntry{
						{
							OID:    "iso.3.6.1.2.1.2.2.1.2",
							Entity: "interface",
							Field:  "name",
						},
						{
							OID:    "iso.3.6.1.2.1.2.2.1.5",
							Entity: "interface",
							Field:  "speed",
						},
						{
							OID:    "iso.3.6.1.2.1.2.2.1.6",
							Entity: "interface",
							Field:  "macAddress",
						},
						{
							OID:    "iso.3.6.1.2.1.2.2.1.7",
							Entity: "interface",
							Field:  "adminStatus",
						},
					},
				},
			},
			objectIDs: mapping.ObjectIDValueMap{
				"iso.3.6.1.2.1.2.2.1.2.999": "GigabitEthernet1/0/1",
				"iso.3.6.1.2.1.2.2.1.5.999": "1000000000",
				"iso.3.6.1.2.1.2.2.1.6.999": "00:00:00:00:00:00",
				"iso.3.6.1.2.1.2.2.1.7.999": "1",
				"iso.3.6.1.2.1.2.2.1.2.555": "GigabitEthernet1/0/1",
				"iso.3.6.1.2.1.2.2.1.5.555": "1000000000",
				"iso.3.6.1.2.1.2.2.1.6.555": "00:00:00:00:00:11",
				"iso.3.6.1.2.1.2.2.1.7.555": "0",
			},
			expected: []diode.Entity{
				&diode.Interface{
					Speed:      &[]int32{1000000000}[0],
					Name:       diode.String("GigabitEthernet1/0/1"),
					MacAddress: &[]string{"00:00:00:00:00:00"}[0],
					Enabled:    &[]bool{true}[0],
				},
				&diode.Interface{
					Speed:      &[]int32{1000000000}[0],
					Name:       diode.String("GigabitEthernet1/0/1"),
					MacAddress: &[]string{"00:00:00:00:00:11"}[0],
					Enabled:    &[]bool{false}[0],
				},
			},
		},
		{
			name: "Not In Mapping",
			mapping: []config.MappingEntry{
				{
					OID:    "1.3.6.1.2.1.4.20.1.1",
					Entity: "ipAddress",
					Field:  "address",
				},
			},
			objectIDs: mapping.ObjectIDValueMap{
				"1.3.6.1.2.1.4.20.1.2": "192.168.1.2",
			},
			expected: []diode.Entity{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper := mapping.NewObjectIDMapper(tt.mapping, slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: false})))
			entities := mapper.MapObjectIDsToEntity(tt.objectIDs)

			assert.ElementsMatch(t, tt.expected, entities)
		})
	}
}

func TestObjectIDs(t *testing.T) {
	tests := []struct {
		name         string
		mapping      []config.MappingEntry
		expectedOIDs []string
	}{
		{
			name: "Single OID",
			mapping: []config.MappingEntry{
				{
					OID:    "1.3.6.1.2.1.4.20.1.1",
					Entity: "ipAddress",
					Field:  "address",
				},
			},
			expectedOIDs: []string{
				"1.3.6.1.2.1.4.20.1.1",
			},
		},
		{
			name: "Duplicate OID",
			mapping: []config.MappingEntry{
				{
					OID:    "iso.3.6.1.2.1.2.2.1",
					Entity: "interface",
					Field:  "_id",
					MappingEntries: []config.MappingEntry{
						{
							OID:    "iso.3.6.1.2.1.2.2.1.2",
							Entity: "inteface",
							Field:  "name",
						},
						{
							OID:    "iso.3.6.1.2.1.2.2.1.5",
							Entity: "inteface",
							Field:  "speed",
						},
					},
				},
			},
			expectedOIDs: []string{
				"iso.3.6.1.2.1.2.2.1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper := mapping.NewObjectIDMapper(tt.mapping, slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: false})))
			objectIDs := mapper.ObjectIDs()

			assert.ElementsMatch(t, tt.expectedOIDs, objectIDs)
		})
	}
}
