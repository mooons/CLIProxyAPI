package redisqueue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const diskExportVersion = 1

type diskExport struct {
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
	Items      [][]byte  `json:"items"`
}

func ExportToDisk(path string) (int, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, fmt.Errorf("usage statistics export path is empty")
	}

	items := SnapshotPayloads()
	if len(items) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("remove empty usage statistics export: %w", err)
		}
		return 0, nil
	}

	payload := diskExport{
		Version:    diskExportVersion,
		ExportedAt: time.Now().UTC(),
		Items:      items,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("marshal usage statistics export: %w", err)
	}

	if err = atomicWriteFile(path, data, 0o600); err != nil {
		return 0, err
	}
	return len(items), nil
}

func LoadFromDisk(path string) (int, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, fmt.Errorf("usage statistics export path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("read usage statistics export: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return 0, nil
	}

	var payload diskExport
	if err = json.Unmarshal(data, &payload); err != nil {
		return 0, fmt.Errorf("parse usage statistics export: %w", err)
	}
	if payload.Version != diskExportVersion {
		return 0, fmt.Errorf("unsupported usage statistics export version: %d", payload.Version)
	}

	return LoadPayloads(payload.Items), nil
}

func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create usage statistics export directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".usage-statistics-*.tmp")
	if err != nil {
		return fmt.Errorf("create usage statistics export temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err = tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod usage statistics export temp file: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write usage statistics export temp file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync usage statistics export temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close usage statistics export temp file: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace usage statistics export: %w", err)
	}
	cleanup = false
	return nil
}
