package data

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type deviceConfig struct {
	Manufacturers []Manufacturer `yaml:"manufacturers"`
	Devices       []Device       `yaml:"devices"`
}

// Manufacturer represents a manufacturer
type Manufacturer struct {
	PrivateEnterpriseNumber int    `yaml:"pen"`
	Name                    string `yaml:"name"`
}

// Device represents a device
type Device struct {
	PrivateEnterpriseNumber int    `yaml:"pen"`
	Name                    string `yaml:"name"`
}

type DeviceDataRetreiver interface {
	GetManufacturer(id int) (Manufacturer, error)
}

// Devices represents a collection of manufacturers
type Devices struct {
	manufacturers map[int]Manufacturer
}

// NewDevices returns a new Devices
func NewDevices(filePath string) (DeviceDataRetreiver, error) {
	devices := deviceConfig{}
	yamlFile, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	yaml.Unmarshal(yamlFile, &devices)

	manufacturersData := make(map[int]Manufacturer)
	for _, manufacturer := range devices.Manufacturers {
		manufacturersData[manufacturer.PrivateEnterpriseNumber] = manufacturer
	}
	return &Devices{
		manufacturers: manufacturersData,
	}, nil
}

// GetDevice returns a manufacturer by its private enterprise number
func (d *Devices) GetManufacturer(id int) (Manufacturer, error) {
	device, ok := d.manufacturers[id]
	if !ok {
		return Manufacturer{}, fmt.Errorf("manufacturer not found")
	}
	return device, nil
}

// NewEmptyDevicesList returns a new empty Devices
func NewEmptyDevicesList() DeviceDataRetreiver {
	return &Devices{
		manufacturers: make(map[int]Manufacturer),
	}
}
