package app

import (
	"errors"

	"github.com/Gerry3010/yate/internal/files"
)

// FileService backs the editor pane.
type FileService struct{}

func (FileService) ServiceName() string { return "FileService" }

func (FileService) Read(path string) (files.Document, error) { return files.Read(path) }

// Write saves; a stale hash yields the error string "changed" so the UI can offer to overwrite.
func (FileService) Write(path, content, expectedHash string) (files.Document, error) {
	d, err := files.Write(path, content, expectedHash)
	if errors.Is(err, files.ErrChanged) {
		return d, errors.New("changed")
	}
	return d, err
}
