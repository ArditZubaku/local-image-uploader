// Package storage offers file system storage functionalities like Saving
package storage

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"time"

	"github.com/ArditZubaku/go-local-image-uploader/internal/utils"
)

// SaveUploadedFiles stores all files from the "files" field into uploadDir.
// It returns the final file names on disk (relative to uploadDir).
func SaveUploadedFiles(uploadDir string, form *multipart.Form) ([]string, error) {
	files := form.File["files"]
	if len(files) == 0 {
		return nil, fmt.Errorf("no files uploaded")
	}

	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating upload dir: %w", err)
	}

	var saved []string

	for _, fh := range files {
		name := filepath.Base(fh.Filename)
		if name == "" {
			continue
		}

		src, err := fh.Open()
		if err != nil {
			return nil, fmt.Errorf("opening uploaded file: %w", err)
		}
		defer utils.CloseOrLog(src, "multipart file")

		timestamp := time.Now().UnixNano()
		finalName := fmt.Sprintf("%d_%s", timestamp, name)
		path := filepath.Join(uploadDir, finalName)

		dst, err := os.Create(path)
		if err != nil {
			return nil, fmt.Errorf("creating destination file: %w", err)
		}

		if _, err := io.Copy(dst, src); err != nil {
			utils.CloseOrLog(dst, "destination file")
			return nil, fmt.Errorf("saving file: %w", err)
		}
		if err := dst.Close(); err != nil {
			return nil, fmt.Errorf("closing file: %w", err)
		}

		saved = append(saved, finalName)
	}

	if len(saved) == 0 {
		return nil, fmt.Errorf("no valid files uploaded")
	}

	return saved, nil
}
