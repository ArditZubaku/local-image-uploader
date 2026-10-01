// Package storage offers file system storage functionalities like Saving
package storage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ArditZubaku/go-local-image-uploader/internal/utils"
)

// SavedFile is the outcome of one part of an upload.
type SavedFile struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Skipped bool   `json:"skipped,omitempty"`
}

// Saver writes incoming multipart parts into a directory. It is safe for
// concurrent use, which matters because the browser now sends several
// upload requests at once.
type Saver struct {
	dir string
}

func NewSaver(dir string) *Saver { return &Saver{dir: dir} }

// SaveStream reads a multipart request one part at a time and writes each
// part straight to disk, so memory use stays constant no matter how large
// a file (or how many files in a folder upload) comes through.
//
// The saved slice is returned even on error: when a phone drops off
// mid-upload, the files that did land are still worth reporting.
func (s *Saver) SaveStream(mr *multipart.Reader) ([]SavedFile, error) {
	var saved []SavedFile
	// MkdirAll is a handful of syscalls; a folder upload would otherwise
	// repeat them for every single file sharing a directory.
	madeDirs := map[string]bool{}

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return saved, fmt.Errorf("reading multipart part: %w", err)
		}

		file, err := s.savePart(part, madeDirs)
		_ = part.Close()
		if err != nil {
			return saved, err
		}
		if file != nil {
			saved = append(saved, *file)
		}
	}

	if len(saved) == 0 {
		return nil, fmt.Errorf("no valid files uploaded")
	}

	return saved, nil
}

func (s *Saver) savePart(part *multipart.Part, madeDirs map[string]bool) (*SavedFile, error) {
	if part.FormName() != "files" || part.FileName() == "" {
		return nil, nil
	}

	// part.FileName() runs the raw header through filepath.Base, which
	// destroys folder-upload paths; read the untouched value ourselves.
	rel, err := sanitizeRelPath(rawFileName(part))
	if err != nil {
		return nil, nil
	}

	dest := filepath.Join(s.dir, rel)

	if dir := filepath.Dir(dest); !madeDirs[dir] {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating upload dir: %w", err)
		}
		madeDirs[dir] = true
	}

	dst, name, err := createUnique(dest)
	if err != nil {
		return nil, err
	}

	written, err := copyBuffered(dst, part)
	if err != nil {
		utils.CloseOrLog(dst, "destination file")
		_ = os.Remove(name)
		return nil, fmt.Errorf("saving %s: %w", rel, err)
	}

	if err := dst.Close(); err != nil {
		return nil, fmt.Errorf("closing file: %w", err)
	}

	out, err := filepath.Rel(s.dir, name)
	if err != nil {
		out = filepath.Base(name)
	}

	return &SavedFile{Name: filepath.ToSlash(out), Size: written}, nil
}

// createUnique opens dest for writing, or the next free " (2)", " (3)"
// variant if it is taken. O_EXCL does the claiming, so two concurrent
// uploads of the same name cannot both win the same file.
func createUnique(dest string) (*os.File, string, error) {
	ext := filepath.Ext(dest)
	stem := strings.TrimSuffix(dest, ext)

	for i := 1; i < 10000; i++ {
		candidate := dest
		if i > 1 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}

		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, candidate, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", fmt.Errorf("creating destination file: %w", err)
		}
	}

	return nil, "", fmt.Errorf("too many files named like %q", filepath.Base(dest))
}

// ManifestItem is one file the browser is about to send.
type ManifestItem struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Needed filters a manifest down to the files not already on disk at the
// same relative path and size.
//
// Re-sending a folder that is already here is the common case (a phone
// re-uploading a camera roll), and asking first means the skipped bytes
// never cross the Wi-Fi at all - a multipart part has to be drained to
// reach the next one, so skipping server-side would save nothing. Size
// alone is a deliberately cheap identity check: hashing would mean reading
// every body anyway, which is exactly the cost being avoided.
func (s *Saver) Needed(items []ManifestItem) []string {
	need := make([]string, 0, len(items))

	for _, it := range items {
		rel, err := sanitizeRelPath(it.Path)
		if err != nil {
			continue
		}

		info, err := os.Stat(filepath.Join(s.dir, rel))
		if err == nil && !info.IsDir() && info.Size() == it.Size {
			continue
		}

		need = append(need, it.Path)
	}

	return need
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
