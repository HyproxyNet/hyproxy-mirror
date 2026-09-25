package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrepareEvictsOldestCacheFile(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "repo", "old.zip")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("123456"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	store := NewLocal(dir, 10)
	file, temp, err := store.Prepare("repo/new.zip", 6)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(temp)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old cache file was not evicted: %v", err)
	}
}

func TestPrepareSkipsAssetLargerThanLimit(t *testing.T) {
	store := NewLocal(t.TempDir(), 10)
	_, _, err := store.Prepare("repo/large.zip", 11)
	if !errors.Is(err, ErrCacheDisabled) {
		t.Fatalf("Prepare error = %v, want ErrCacheDisabled", err)
	}
}
