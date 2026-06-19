//go:build !linux

package csi

import "fmt"

func isMountPoint(string) (bool, error) {
	return false, nil
}

func bindMount(source, target string, readOnly bool) error {
	return fmt.Errorf("bind mount from %q to %q readOnly=%t is only supported on linux", source, target, readOnly)
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

	return nil
}
