package csi

import (
	"context"
	"testing"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
)

func TestGetPluginInfo(t *testing.T) {
	driver := NewDriver(Config{DriverName: "csi.praesto.io"})
	server := &identityServer{driver: driver}

	response, err := server.GetPluginInfo(context.Background(), &csipb.GetPluginInfoRequest{})
	if err != nil {
		t.Fatalf("GetPluginInfo returned error: %v", err)
	}

	if response.Name != "csi.praesto.io" {
		t.Fatalf("expected driver name %q, got %q", "csi.praesto.io", response.Name)
	}
}

func TestProbe(t *testing.T) {
	driver := NewDriver(Config{DriverName: "csi.praesto.io"})
	server := &identityServer{driver: driver}

	_, err := server.Probe(context.Background(), &csipb.ProbeRequest{})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
}

func TestGetPluginCapabilities(t *testing.T) {
	driver := NewDriver(Config{DriverName: "csi.praesto.io"})
	server := &identityServer{driver: driver}

	response, err := server.GetPluginCapabilities(context.Background(), &csipb.GetPluginCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("GetPluginCapabilities returned error: %v", err)
	}

	if len(response.Capabilities) != 0 {
		t.Fatalf("expected no plugin capabilities, got %d", len(response.Capabilities))
	}
}
