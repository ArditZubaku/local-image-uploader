// Package storage offers file system storage functionalities like Saving
package storage

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one file or folder inside a browsed directory.
type Entry struct {
	Name    string
	Rel     string // slash-separated path from the share root, for building links
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// ResolveWithinRoot cleans a client-supplied relative path and resolves it
// under root, refusing to let it escape root via ".." or an absolute path.
// An empty rel resolves to root itself.
func ResolveWithinRoot(root, rel string) (string, error) {
	if rel == "" {
		return root, nil
	}

	clean := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	clean = strings.TrimPrefix(clean, "/")

	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe path: %q", rel)
	}
	if clean == "." {
		return root, nil
	}

	return filepath.Join(root, filepath.FromSlash(clean)), nil
}

// ListDir returns the entries of the directory at rel (under root),
// directories first, then alphabetically within each group.
func ListDir(root, rel string) ([]Entry, error) {
	full, err := ResolveWithinRoot(root, rel)
	if err != nil {
		return nil, err
	}

	dirEntries, err := os.ReadDir(full)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		info, err := de.Info()
		if err != nil {
			continue
		}

		entries = append(entries, Entry{
			Name:    de.Name(),
			Rel:     path.Join(filepath.ToSlash(rel), de.Name()),
			IsDir:   de.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return entries, nil
}

// WriteZip streams the directory at rel (under root) into w as a zip
// archive, one file at a time, so folders much larger than available
// memory can still be downloaded.
func WriteZip(w io.Writer, root, rel string) error {
	full, err := ResolveWithinRoot(root, rel)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	defer zw.Close()

	return filepath.WalkDir(full, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(full, p)
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(relPath)
		// Store, not Deflate: source files are typically already-compressed
		// images/video, so compressing again just burns CPU for ~0 size
		// win and would bottleneck a large streamed download.
		hdr.Method = zip.Store

		dst, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}

		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()

		_, err = io.Copy(dst, src)
		return err
	})
}
