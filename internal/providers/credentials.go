package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func updateJSONCredentials(path string, update func(map[string]any) error) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat credentials file %s: %w", path, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read credentials file %s: %w", path, err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse credentials file %s: %w", path, err)
	}

	if err := update(raw); err != nil {
		return err
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("failed to encode credentials file %s: %w", path, err)
	}

	if err := writeFileAtomic(path, encoded, info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to write credentials file %s: %w", path, err)
	}

	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-cred-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}

func formatCredentialTime(ts time.Time) string {
	return ts.UTC().Format(time.RFC3339Nano)
}
