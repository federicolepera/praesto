package csi

import (
	"os"
	"path/filepath"
)

const completeFileName = ".praesto-complete"

func directoryExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func cacheComplete(path string) (bool, error) {
	info, err := os.Stat(filepath.Join(path, completeFileName))
	if err == nil {
		return !info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
