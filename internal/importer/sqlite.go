package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// sqliteCLI queries a database with the sqlite3 command-line tool (present on macOS and most
// Linux installs), so wate needs no sqlite driver. The database and its WAL/SHM side files are
// copied to a temp dir first: the source app may hold the live file open and locked.
func sqliteCLI(db, query string) ([]map[string]any, error) {
	tmp, err := os.MkdirTemp("", "wate-import-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	copyTo := filepath.Join(tmp, "db.sqlite")
	if err := copyFile(db, copyTo); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if exists(db + suffix) {
			_ = copyFile(db+suffix, copyTo+suffix)
		}
	}
	cmd := exec.Command("sqlite3", "-json", "-readonly", copyTo, query)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sqlite3: %v: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	if bytes.TrimSpace(out.Bytes()) == nil {
		return nil, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		return nil, fmt.Errorf("sqlite3 output: %w", err)
	}
	return rows, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// rowInt/rowStr/rowFloat read loosely typed JSON cells.
func rowInt(r map[string]any, k string) int {
	switch v := r[k].(type) {
	case float64:
		return int(v)
	case bool:
		if v {
			return 1
		}
	}
	return 0
}

func rowStr(r map[string]any, k string) string {
	if s, ok := r[k].(string); ok {
		return s
	}
	return ""
}

func rowFloat(r map[string]any, k string) float64 {
	if f, ok := r[k].(float64); ok {
		return f
	}
	return 0
}
