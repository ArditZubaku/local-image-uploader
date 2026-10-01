// Package storage offers file system storage functionalities like Saving
package storage

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArditZubaku/go-local-image-uploader/internal/utils"
)

// SaveStream reads a multipart request one part at a time and writes each
// part straight to disk, so memory use stays constant no matter how large
// a file (or how many files in a folder upload) comes through.
func SaveStream(uploadDir string, mr *multipart.Reader) ([]string, error) {
	var saved []string

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return saved, fmt.Errorf("reading multipart part: %w", err)
		}

		name, err := savePart(uploadDir, part)
		_ = part.Close()
		if err != nil {
			return saved, err
		}
		if name != "" {
			saved = append(saved, name)
		}
	}

	if len(saved) == 0 {
		return nil, fmt.Errorf("no valid files uploaded")
	}

	return saved, nil
}

func savePart(uploadDir string, part *multipart.Part) (string, error) {
	if part.FormName() != "files" || part.FileName() == "" {
		return "", nil
	}

	// part.FileName() runs the raw header through filepath.Base, which
	// destroys folder-upload paths; read the untouched value ourselves.
	rel, err := sanitizeRelPath(rawFileName(part))
	if err != nil {
		return "", nil
	}

	dir, base := filepath.Split(rel)
	relOut := filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), base))
	dest := filepath.Join(uploadDir, relOut)

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("creating upload dir: %w", err)
	}

	dst, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("creating destination file: %w", err)
	}

	if _, err := io.Copy(dst, part); err != nil {
		utils.CloseOrLog(dst, "destination file")
		return "", fmt.Errorf("saving %s: %w", rel, err)
	}

	if err := dst.Close(); err != nil {
		return "", fmt.Errorf("closing file: %w", err)
	}

	return filepath.ToSlash(relOut), nil
}

func rawFileName(part *multipart.Part) string {
	_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil {
		return part.FileName()
	}
	if fn, ok := params["filename"]; ok {
		return fn
	}
	return part.FileName()
}

// sanitizeRelPath cleans a client-supplied (possibly multi-segment) path
// and rejects anything that would escape uploadDir.
func sanitizeRelPath(raw string) (string, error) {
	clean := path.Clean(strings.ReplaceAll(raw, "\\", "/"))
	clean = strings.TrimPrefix(clean, "/")

	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe path: %q", raw)
	}

	return filepath.FromSlash(clean), nil
}
