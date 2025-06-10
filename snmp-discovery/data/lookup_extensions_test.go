package data

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManufacturerLookup(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name:    "successful creation",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup, err := NewManufacturerLookup()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, lookup)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, lookup)
				assert.NotNil(t, lookup.data)
			}
		})
	}
}

func TestManufacturerLookup_GetManufacturer(t *testing.T) {
	// Create a manufacturer lookup instance
	lookup, err := NewManufacturerLookup()
	require.NoError(t, err)
	require.NotNil(t, lookup)

	tests := []struct {
		name    string
		id      int
		want    string
		wantErr bool
	}{
		{
			name:    "existing manufacturer - Reserved",
			id:      0,
			want:    "Reserved",
			wantErr: false,
		},
		{
			name:    "existing manufacturer - IBM",
			id:      2,
			want:    "IBM httpsw3ibmcomstandards",
			wantErr: false,
		},
		{
			name:    "existing manufacturer - Cisco Systems",
			id:      9,
			want:    "ciscoSystems",
			wantErr: false,
		},
		{
			name:    "existing manufacturer - Hewlett Packard",
			id:      11,
			want:    "HewlettPackard",
			wantErr: false,
		},
		{
			name:    "existing manufacturer - Apple Computer Inc",
			id:      63,
			want:    "Apple Computer Inc",
			wantErr: false,
		},
		{
			name:    "non-existing manufacturer - negative ID",
			id:      -1,
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-existing manufacturer - large ID",
			id:      999999,
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-existing manufacturer - zero ID that doesn't exist",
			id:      100000,
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lookup.GetManufacturer(tt.id)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, "", got)
				assert.Contains(t, err.Error(), "manufacturer not found")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestManufacturerLookup_GetManufacturer_DataIntegrity(t *testing.T) {
	// Test that the embedded data is properly loaded and accessible
	lookup, err := NewManufacturerLookup()
	require.NoError(t, err)
	require.NotNil(t, lookup)

	// Check that we have a reasonable number of manufacturers
	dataSize := len(*lookup.data)
	assert.Greater(t, dataSize, 1000, "Expected at least 1000 manufacturers in the data")

	// Test some well-known manufacturers that should exist
	wellKnownManufacturers := map[int]string{
		9:   "ciscoSystems",
		43:  "3Com",
		161: "Motorola",
		11:  "HewlettPackard",
	}

	for id, expectedName := range wellKnownManufacturers {
		t.Run("well-known manufacturer "+expectedName, func(t *testing.T) {
			manufacturer, err := lookup.GetManufacturer(id)
			assert.NoError(t, err)
			assert.Equal(t, expectedName, manufacturer)
		})
	}
}

func TestManufacturerLookup_EdgeCases(t *testing.T) {
	lookup, err := NewManufacturerLookup()
	require.NoError(t, err)

	// Test boundary conditions
	tests := []struct {
		name string
		id   int
	}{
		{"zero ID", 0},
		{"max int32", 2147483647},
		{"min int32", -2147483648},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We don't assert specific results here since we don't know
			// what IDs exist, but we ensure the function doesn't panic
			_, err := lookup.GetManufacturer(tt.id)
			// Error is acceptable for non-existent IDs
			if err != nil {
				assert.Contains(t, err.Error(), "manufacturer not found")
			}
		})
	}
}
