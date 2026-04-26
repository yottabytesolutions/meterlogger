package main

import (
	"os"
	"testing"

	"github.com/spf13/viper"
)

func TestInitConfig_DefaultsNoFile(t *testing.T) {
	// Reset viper state
	viper.Reset()
	cfgFile = ""

	// Run initConfig with no config file present
	// It should not panic; it sets defaults and tries to read config (harmlessly failing)
	initConfig()

	// Default should be false
	if viper.GetBool("Debug") != false {
		t.Error("initConfig() should set Debug default to false")
	}
}

func TestInitConfig_WithExplicitFile(t *testing.T) {
	// Create a temp config file
	f, err := os.CreateTemp(t.TempDir(), "meterlogger-test-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	_, _ = f.WriteString("Debug: true\n")
	_ = f.Close()

	viper.Reset()
	cfgFile = f.Name()
	defer func() { cfgFile = "" }()

	initConfig()

	if !viper.GetBool("Debug") {
		t.Error("initConfig() should load Debug=true from config file")
	}
}

func TestInitConfig_EnabledFields(t *testing.T) {
	yamlContent := `
Heat:
  Enabled: true
  Measurement: heat_meter
  SerialInterface: /dev/ttyUSB0
  MbusAddress: 1
  ScrapeInterval: 30s
Grid:
  Enabled: false
  Measurement: grid_meter
  SerialInterface: /dev/ttyUSB1
Enphase:
  Enabled: false
  Measurement: solar
Ventilation:
  Enabled: true
  MeasurementBaseName: ventilation
  ScrapeInterval: 1m
  HostUrl: http://192.168.1.200
`
	f, err := os.CreateTemp(t.TempDir(), "meterlogger-enabled-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	_, _ = f.WriteString(yamlContent)
	_ = f.Close()

	viper.Reset()
	cfgFile = f.Name()
	defer func() { cfgFile = "" }()

	initConfig()

	if !config.Heat.Enabled {
		t.Error("Heat.Enabled should be true")
	}
	if config.Grid.Enabled {
		t.Error("Grid.Enabled should be false")
	}
	if config.Enphase.Enabled {
		t.Error("Enphase.Enabled should be false")
	}
	if !config.Ventilation.Enabled {
		t.Error("Ventilation.Enabled should be true")
	}
}
