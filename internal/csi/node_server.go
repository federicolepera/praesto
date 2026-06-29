package csi

import (
	"context"
	"fmt"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	volumeAttributeModelCacheNamespace = "modelCacheNamespace"
	volumeAttributeModelCacheName      = "modelCacheName"
)

type nodeServer struct {
	csipb.UnimplementedNodeServer
	driver *Driver
}

func (s *nodeServer) NodeGetInfo(context.Context, *csipb.NodeGetInfoRequest) (*csipb.NodeGetInfoResponse, error) {
	return &csipb.NodeGetInfoResponse{
		NodeId: s.driver.config.NodeName,
	}, nil
}

func (s *nodeServer) NodeGetCapabilities(context.Context, *csipb.NodeGetCapabilitiesRequest) (*csipb.NodeGetCapabilitiesResponse, error) {
	return &csipb.NodeGetCapabilitiesResponse{}, nil
}

func (s *nodeServer) NodePublishVolume(ctx context.Context, req *csipb.NodePublishVolumeRequest) (*csipb.NodePublishVolumeResponse, error) {
	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "target path is required")
	}

	volumeContext := req.GetVolumeContext()
	modelCacheNamespace := volumeContext[volumeAttributeModelCacheNamespace]
	if modelCacheNamespace == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s is required", volumeAttributeModelCacheNamespace)
	}

	modelCacheName := volumeContext[volumeAttributeModelCacheName]
	if modelCacheName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s is required", volumeAttributeModelCacheName)
	}

	sourcePath := sourcePathForModelCache(s.driver.config.CacheRoot, modelCacheNamespace, modelCacheName)
	exists, err := directoryExists(sourcePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check model cache source path %q: %v", sourcePath, err)
	}
	if !exists {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("model cache source path %q does not exist", sourcePath))
	}
	complete, err := cacheComplete(sourcePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check model cache complete marker in %q: %v", sourcePath, err)
	}
	if !complete {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("model cache source path %q is not ready: missing %s", sourcePath, completeFileName))
	}

	if err := bindMount(sourcePath, targetPath, req.GetReadonly()); err != nil {
		return nil, status.Errorf(codes.Internal, "publish model cache volume from %q to %q: %v", sourcePath, targetPath, err)
	}

	return &csipb.NodePublishVolumeResponse{}, nil
}

func (s *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csipb.NodeUnpublishVolumeRequest) (*csipb.NodeUnpublishVolumeResponse, error) {
	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "target path is required")
	}

	if err := unmount(targetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "unpublish volume from %q: %v", targetPath, err)
	}

	return &csipb.NodeUnpublishVolumeResponse{}, nil
}
