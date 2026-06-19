package csi

import (
	"path/filepath"
)

func sourcePathForModelCache(cacheRoot, namespace, name string) string {
	return filepath.Join(cacheRoot, namespace, name)
}