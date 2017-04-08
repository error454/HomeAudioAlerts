package main

import (
	"testing"
)

func TestConfigLoading(t *testing.T) {
	t.Log("Testing config loading")

	err := readConfigFromDisk("testData/config")

	if err != nil {
		t.Error("Config failed: " + err.Error())
		return
	}

	if config.LmsIP != "10.10.0.1:9090" {
		t.Error("LmsIP failed")
	}

	if config.WebPort != "8005" {
		t.Error("WebPort failed")
	}

	if len(config.Zones) != 2 {
		t.Error("Zone count failed")
	}

	if config.Zones["Zone1"].AlsaName != "GarageMusic" {
		t.Error("Zone 1 AlsaName failed")
	}

	if config.Zones["Zone1"].MAC != "00:01:02:03:04:05" {
		t.Error("Zone 1 MAC failed")
	}

	if config.Zones["Zone2"].AlsaName != "LivingRoom" {
		t.Error("Zone 1 AlsaName failed")
	}

	if config.Zones["Zone2"].MAC != "00:01:02:03:04:06" {
		t.Error("Zone 1 MAC failed")
	}
}

func TestZoneParameter(t *testing.T) {
	t.Log("Testing zoneParameterEnabled")

	if zoneParameterEnabled("") == true {
		t.Error("Got true from nil")
	}

	if zoneParameterEnabled("1") == false {
		t.Error("Got false from 1")
	}

	if zoneParameterEnabled("2") == true {
		t.Error("Got true from 2")
	}

	if zoneParameterEnabled("asdf as") == true {
		t.Error("Got true from text string")
	}
}
