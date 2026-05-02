package redisqueue

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExportToDiskAndLoadFromDiskRoundTrip(t *testing.T) {
	prevEnabled := Enabled()
	SetEnabled(false)
	SetEnabled(true)
	t.Cleanup(func() {
		SetEnabled(false)
		SetEnabled(prevEnabled)
	})

	Enqueue([]byte(`{"provider":"openai","model":"gpt-5.4","total_tokens":3}`))
	Enqueue([]byte(`{"provider":"gemini","model":"gemini-3-pro","total_tokens":5}`))

	path := filepath.Join(t.TempDir(), "usage-statistics-export.json")
	count, err := ExportToDisk(path)
	if err != nil {
		t.Fatalf("ExportToDisk returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("ExportToDisk count = %d, want 2", count)
	}

	SetEnabled(false)
	SetEnabled(true)

	loaded, err := LoadFromDisk(path)
	if err != nil {
		t.Fatalf("LoadFromDisk returned error: %v", err)
	}
	if loaded != 2 {
		t.Fatalf("LoadFromDisk count = %d, want 2", loaded)
	}

	items := PopOldest(10)
	if len(items) != 2 {
		t.Fatalf("PopOldest items = %d, want 2", len(items))
	}
	if string(items[0]) != `{"provider":"openai","model":"gpt-5.4","total_tokens":3}` {
		t.Fatalf("first payload = %s", string(items[0]))
	}
	if string(items[1]) != `{"provider":"gemini","model":"gemini-3-pro","total_tokens":5}` {
		t.Fatalf("second payload = %s", string(items[1]))
	}
}

func TestExportToDiskRemovesEmptyExport(t *testing.T) {
	prevEnabled := Enabled()
	SetEnabled(false)
	SetEnabled(true)
	t.Cleanup(func() {
		SetEnabled(false)
		SetEnabled(prevEnabled)
	})

	path := filepath.Join(t.TempDir(), "usage-statistics-export.json")
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale export: %v", err)
	}

	count, err := ExportToDisk(path)
	if err != nil {
		t.Fatalf("ExportToDisk returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("ExportToDisk count = %d, want 0", count)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("export file still exists, stat err=%v", err)
	}
}
