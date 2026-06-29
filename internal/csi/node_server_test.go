package csi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNodeGetInfo(t *testing.T) {
	driver := NewDriver(Config{NodeName: "minikube"})
	server := &nodeServer{driver: driver}

	response, err := server.NodeGetInfo(context.Background(), &csipb.NodeGetInfoRequest{})
	if err != nil {
		t.Fatalf("NodeGetInfo returned error: %v", err)
	}

	if response.NodeId != "minikube" {
		t.Fatalf("expected node id %q, got %q", "minikube", response.NodeId)
	}
}

func TestNodeGetCapabilities(t *testing.T) {
	driver := NewDriver(Config{NodeName: "minikube"})
	server := &nodeServer{driver: driver}

	response, err := server.NodeGetCapabilities(context.Background(), &csipb.NodeGetCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("NodeGetCapabilities returned error: %v", err)
	}

	if len(response.Capabilities) != 0 {
		t.Fatalf("expected no node capabilities, got %d", len(response.Capabilities))
	}
}

func TestNodePublishVolumeRequiresTargetPath(t *testing.T) {
	driver := NewDriver(Config{CacheRoot: t.TempDir()})
	server := &nodeServer{driver: driver}

	_, err := server.NodePublishVolume(context.Background(), &csipb.NodePublishVolumeRequest{
		VolumeContext: map[string]string{
			volumeAttributeModelCacheNamespace: "default",
			volumeAttributeModelCacheName:      "tinyllama-test",
		},
	})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument error, got %v", err)
	}
}

func TestNodePublishVolumeRequiresModelCacheNamespace(t *testing.T) {
	driver := NewDriver(Config{CacheRoot: t.TempDir()})
	server := &nodeServer{driver: driver}

	_, err := server.NodePublishVolume(context.Background(), &csipb.NodePublishVolumeRequest{
		TargetPath: filepath.Join(t.TempDir(), "target"),
		VolumeContext: map[string]string{
			volumeAttributeModelCacheName: "tinyllama-test",
		},
	})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument error, got %v", err)
	}
}

func TestNodePublishVolumeRequiresModelCacheName(t *testing.T) {
	driver := NewDriver(Config{CacheRoot: t.TempDir()})
	server := &nodeServer{driver: driver}

	_, err := server.NodePublishVolume(context.Background(), &csipb.NodePublishVolumeRequest{
		TargetPath: filepath.Join(t.TempDir(), "target"),
		VolumeContext: map[string]string{
			volumeAttributeModelCacheNamespace: "default",
		},
	})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument error, got %v", err)
	}
}

func TestNodePublishVolumeReturnsNotFoundForMissingSourcePath(t *testing.T) {
	driver := NewDriver(Config{CacheRoot: t.TempDir()})
	server := &nodeServer{driver: driver}

	_, err := server.NodePublishVolume(context.Background(), &csipb.NodePublishVolumeRequest{
		TargetPath: filepath.Join(t.TempDir(), "target"),
		VolumeContext: map[string]string{
			volumeAttributeModelCacheNamespace: "default",
			volumeAttributeModelCacheName:      "tinyllama-test",
		},
	})

	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound error, got %v", err)
	}
}

func TestNodePublishVolumeRequiresCompleteMarker(t *testing.T) {
	cacheRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cacheRoot, "default", "tinyllama-test"), 0o755); err != nil {
		t.Fatalf("create source path: %v", err)
	}
	driver := NewDriver(Config{CacheRoot: cacheRoot})
	server := &nodeServer{driver: driver}

	_, err := server.NodePublishVolume(context.Background(), &csipb.NodePublishVolumeRequest{
		TargetPath: filepath.Join(t.TempDir(), "target"),
		VolumeContext: map[string]string{
			volumeAttributeModelCacheNamespace: "default",
			volumeAttributeModelCacheName:      "tinyllama-test",
		},
	})

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition error, got %v", err)
	}
}

func TestNodeUnpublishVolumeRequiresTargetPath(t *testing.T) {
	driver := NewDriver(Config{})
	server := &nodeServer{driver: driver}

	_, err := server.NodeUnpublishVolume(context.Background(), &csipb.NodeUnpublishVolumeRequest{})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument error, got %v", err)
	}
}

func TestNodeUnpublishVolumeSucceedsForMissingTargetPath(t *testing.T) {
	driver := NewDriver(Config{})
	server := &nodeServer{driver: driver}

	_, err := server.NodeUnpublishVolume(context.Background(), &csipb.NodeUnpublishVolumeRequest{
		TargetPath: filepath.Join(t.TempDir(), "missing"),
	})
	if err != nil {
		t.Fatalf("NodeUnpublishVolume returned error: %v", err)
	}
}

func TestNodeUnpublishVolumeSucceedsForExistingNonMountDirectory(t *testing.T) {
	driver := NewDriver(Config{})
	server := &nodeServer{driver: driver}

	_, err := server.NodeUnpublishVolume(context.Background(), &csipb.NodeUnpublishVolumeRequest{
		TargetPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NodeUnpublishVolume returned error: %v", err)
	}
}
