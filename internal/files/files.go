// Package files is the editor's file access: read, write, and change detection.
package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// MaxSize guards the editor from multi-hundred-MB logs.
const MaxSize = 8 << 20

// Document is a file's content plus what we need to detect concurrent edits.
type Document struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"`
	// Hash of the content on disk; Write refuses when it changed under us unless forced.
	Hash    string `json:"hash"`
	ModTime int64  `json:"mod_time"`
	Size    int64  `json:"size"`
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// Read loads a text file.
func Read(path string) (Document, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Document{}, err
	}
	if st.IsDir() {
		return Document{}, fmt.Errorf("%s is a directory", path)
	}
	if st.Size() > MaxSize {
		return Document{}, fmt.Errorf("%s is too large for the editor (%d MB limit)", filepath.Base(path), MaxSize>>20)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	if !utf8.Valid(b) {
		return Document{}, fmt.Errorf("%s is not valid UTF-8 text", filepath.Base(path))
	}
	return Document{Path: path, Name: filepath.Base(path), Content: string(b), Hash: hashOf(b), ModTime: st.ModTime().UnixMilli(), Size: st.Size()}, nil
}

// ErrChanged is returned by Write when the file on disk no longer matches the hash the editor loaded.
var ErrChanged = errors.New("file changed on disk")

// Write saves content. When expectedHash is non-empty and the file changed since, ErrChanged is returned.
func Write(path, content, expectedHash string) (Document, error) {
	if expectedHash != "" {
		if cur, err := os.ReadFile(path); err == nil && hashOf(cur) != expectedHash {
			return Document{}, ErrChanged
		}
	}
	st, statErr := os.Stat(path)
	mode := os.FileMode(0o644)
	if statErr == nil {
		mode = st.Mode().Perm()
	}
	// Write to a temp file in the same directory and rename: no torn files on crash.
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".wate-*")
	if err != nil {
		return Document{}, err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return Document{}, err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return Document{}, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return Document{}, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return Document{}, err
	}
	return Read(path)
}
