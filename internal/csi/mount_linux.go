package csi

import (
	"fmt"
	"os"

	mountutils "k8s.io/mount-utils"
)

// Mount helpers will live here.
//
// The first implementation provides idempotent bind mount/unmount helpers used
// by NodePublishVolume and NodeUnpublishVolume.
func isMountPoint(path string) (bool, error) {
	mounter := mountutils.New("")
	notMountPoint, err := mounter.IsLikelyNotMountPoint(path)
	if err != nil {
		return false, err
	}
	return !notMountPoint, nil
}

func bindMount(source, target string, readOnly bool) error {
	exists, err := directoryExists(source)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("source directory %q does not exist", source)
	}

	if err := os.MkdirAll(target, 0o750); err != nil {
		return fmt.Errorf("failed to create target directory %q: %w", target, err)
	}

	mounted, err := isMountPoint(target)
	if err != nil {
		return fmt.Errorf("failed to check if target directory %q is a mount point: %w", target, err)
	}
	if mounted {
		return nil
	}

	mounter := mountutils.New("")
	if err := mounter.Mount(source, target, "", []string{"bind"}); err != nil {
		return fmt.Errorf("failed to bind mount %q to %q: %w", source, target, err)
	}

	if readOnly {
		if err := mounter.Mount(source, target, "", []string{"bind", "remount", "ro"}); err != nil {
			return fmt.Errorf("failed to remount %q as read-only: %w", target, err)
		}
	}

	return nil
}

func unmount(target string) error {
	exists, err := directoryExists(target)
	if err != nil {
		return fmt.Errorf("failed to check target directory %q: %w", target, err)
	}
	if !exists {
		return nil
	}

	mounted, err := isMountPoint(target)
	if err != nil {
		return fmt.Errorf("failed to check if target directory %q is a mount point: %w", target, err)
	}

	if !mounted {
		return nil
	}

	mounter := mountutils.New("")
	if err := mounter.Unmount(target); err != nil {
		return fmt.Errorf("failed to unmount %q: %w", target, err)
	}

	return nil
}
