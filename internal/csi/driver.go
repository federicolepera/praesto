package csi

import (
	"fmt"
	"net"
	"os"
	"strings"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
)

const DefaultDriverName = "csi.praesto.io"

type Config struct {
	DriverName string
	Endpoint   string
	NodeName   string
	CacheRoot  string
}

type Driver struct {
	config Config
}

func NewDriver(config Config) *Driver {
	if config.DriverName == "" {
		config.DriverName = DefaultDriverName
	}
	if config.Endpoint == "" {
		config.Endpoint = "unix:///csi/csi.sock"
	}
	if config.CacheRoot == "" {
		config.CacheRoot = "/var/praesto"
	}

	return &Driver{config: config}
}

func (d *Driver) Run() error {
	listener, err := listenUnix(d.config.Endpoint)
	if err != nil {
		return err
	}

	server := grpc.NewServer()
	csipb.RegisterIdentityServer(server, &identityServer{driver: d})
	csipb.RegisterNodeServer(server, &nodeServer{driver: d})

	return server.Serve(listener)
}

func listenUnix(endpoint string) (net.Listener, error) {
	path := strings.TrimPrefix(endpoint, "unix://")
	if path == endpoint {
		return nil, fmt.Errorf("only unix:// endpoints are supported, got %q", endpoint)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return net.Listen("unix", path)
}
