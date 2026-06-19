package csi

import (
	"context"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
)

type identityServer struct {
	csipb.UnimplementedIdentityServer
	driver *Driver
}

func (s *identityServer) GetPluginInfo(context.Context, *csipb.GetPluginInfoRequest) (*csipb.GetPluginInfoResponse, error) {
	return &csipb.GetPluginInfoResponse{
		Name: s.driver.config.DriverName,
	}, nil
}

func (s *identityServer) Probe(context.Context, *csipb.ProbeRequest) (*csipb.ProbeResponse, error) {
	return &csipb.ProbeResponse{}, nil
}

func (s *identityServer) GetPluginCapabilities(context.Context, *csipb.GetPluginCapabilitiesRequest) (*csipb.GetPluginCapabilitiesResponse, error) {
	return &csipb.GetPluginCapabilitiesResponse{}, nil
}