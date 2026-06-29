package csi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryExistsForExistingDirectory(t *testing.T) {
	dir := t.TempDir()

	exists, err := directoryExists(dir)
	if err != nil {
		t.Fatalf("directoryExists returned error: %v", err)
	}
	if !exists {
		t.Fatalf("expected directory to exist")
	}
}

func TestDirectoryExistsForMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")

	exists, err := directoryExists(path)
	if err != nil {
		t.Fatalf("directoryExists returned error: %v", err)
	}
	if exists {
		t.Fatalf("expected path to not exist")
	}
}

func TestDirectoryExistsForFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("test"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	exists, err := directoryExists(file)
	if err != nil {
		t.Fatalf("directoryExists returned error: %v", err)
	}
	if exists {
		t.Fatalf("expected file path to not count as directory")
	}
}

func TestCacheComplete(t *testing.T) {
	dir := t.TempDir()

	complete, err := cacheComplete(dir)
	if err != nil {
		t.Fatalf("cacheComplete returned error: %v", err)
	}
	if complete {
		t.Fatalf("expected cache to be incomplete without marker")
	}

	if err := os.WriteFile(filepath.Join(dir, completeFileName), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write complete marker: %v", err)
	}
	complete, err = cacheComplete(dir)
	if err != nil {
		t.Fatalf("cacheComplete returned error: %v", err)
	}
	if !complete {
		t.Fatalf("expected cache to be complete with marker")
	}
}
