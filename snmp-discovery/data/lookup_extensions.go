package data

import (
	"bufio"
	"embed"
	"fmt"
	"log"
	"strconv"
	"strings"
)

//go:embed manufacturers.yaml
var manufacturersData embed.FS

// ManufacturerRetriever is an interface that provides a method to retrieve a manufacturer by ID
type ManufacturerRetriever interface {
	GetManufacturer(id int) (string, error)
}

// ManufacturerLookup represents a manufacturer lookup service
type ManufacturerLookup struct {
	data *map[int]string
}

// NewManufacturerLookup creates a new manufacturer lookup service
func NewManufacturerLookup() (*ManufacturerLookup, error) {
	file, err := manufacturersData.Open("manufacturers.yaml")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Println("Error closing file:", err)
		}
	}()

	manufacturers := make(map[int]string)
	// Don't use yaml.Unmarshal because it is way too slow for a large file
	scanner := bufio.NewScanner(file)

	// Skip the first line which is "manufacturers:"
	_ = scanner.Scan()

	// Parse each line
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Parse lines in format "  ID: Name"
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		idStr := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])

		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue // Skip invalid entries
		}

		manufacturers[id] = name
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &ManufacturerLookup{
		data: &manufacturers,
	}, nil
}

// GetManufacturer returns the manufacturer name for a given ID
func (m *ManufacturerLookup) GetManufacturer(id int) (string, error) {
	if name, ok := (*m.data)[id]; ok {
		return name, nil
	}
	return "", fmt.Errorf("manufacturer not found")
}
