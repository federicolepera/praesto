package csi

import (
	"path/filepath"
	"testing"
)

func TestSourcePathForModelCache(t *testing.T) {
	got := sourcePathForModelCache("/var/praesto", "default", "tinyllama-test")
	want := filepath.Join("/var/praesto", "default", "tinyllama-test")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}