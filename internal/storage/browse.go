// Package storage offers file system storage functionalities like Saving
package storage

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
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

// Root is a share directory that client-supplied paths are resolved against.
// It keeps the symlink-resolved form of the directory so that Resolve can
// reject paths escaping through a symlink, not just through "..".
type Root struct {
	real string
}

func NewRoot(dir string) (*Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// A share dir that doesn't exist yet is fine; it gets created on
		// the first upload. Fall back to the lexical path until then.
		if errors.Is(err, fs.ErrNotExist) {
			return &Root{real: abs}, nil
		}
		return nil, err
	}

	return &Root{real: real}, nil
}

func (r *Root) Dir() string { return r.real }

// Resolve cleans a client-supplied relative path and resolves it under the
// root, refusing anything that escapes via "..", an absolute path, or a
// symlink. An empty rel resolves to the root itself.
func (r *Root) Resolve(rel string) (string, error) {
	full, err := resolveLexically(r.real, rel)
	if err != nil {
		return "", err
	}

	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	if real != r.real && !strings.HasPrefix(real, r.real+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes share root: %q", rel)
	}

	return real, nil
}

func resolveLexically(root, rel string) (string, error) {
	if rel == "" {
		return root, nil
	}

	clean := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	clean = strings.TrimPrefix(clean, "/")

	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe path: %q", rel)
	}
	if clean == "." || clean == "" {
		return root, nil
	}

	return filepath.Join(root, filepath.FromSlash(clean)), nil
}

// ListDir returns the entries of the directory at rel (under root),
// directories first, then alphabetically within each group. It returns at
// most limit entries; a camera-roll folder with thousands of files would
// otherwise render a page large enough to stall the phone showing it.
func (r *Root) ListDir(rel string, limit int) ([]Entry, int, error) {
	full, err := r.Resolve(rel)
	if err != nil {
		return nil, 0, err
	}

	dirEntries, err := os.ReadDir(full)
	if err != nil {
		return nil, 0, fmt.Errorf("reading directory: %w", err)
	}

	relSlash := strings.Trim(filepath.ToSlash(rel), "/")

	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		e := Entry{
			Name:  de.Name(),
			Rel:   path.Join(relSlash, de.Name()),
			IsDir: de.IsDir(),
		}

		// ReadDir reports a symlink's own type, so a link to a folder would
		// otherwise be listed as an unopenable file. Following it costs one
		// stat, and only for the links.
		if de.Type()&fs.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(full, de.Name()))
			if err != nil {
				continue
			}
			e.IsDir = info.IsDir()
			e.Size = info.Size()
			e.ModTime = info.ModTime()
			entries = append(entries, e)
			continue
		}

		// Only files need a stat: the size and mtime of a directory are
		// never shown, and skipping it saves a syscall per folder.
		if !e.IsDir {
			info, err := de.Info()
			if err != nil {
				continue
			}
			e.Size = info.Size()
			e.ModTime = info.ModTime()
		}

		entries = append(entries, e)
	}

	slices.SortFunc(entries, func(a, b Entry) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	total := len(entries)
	if limit > 0 && total > limit {
		entries = entries[:limit]
	}

	return entries, total, nil
}

// ZipEntry is one file staged for an archive, with the size that was
// stat'd while planning.
type ZipEntry struct {
	Name    string // slash-separated path inside the archive
	Path    string // on-disk path
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
}

// ZipPlan is a fully enumerated archive: every member and the exact number
// of bytes WriteTo will emit for them.
type ZipPlan struct {
	Entries []ZipEntry
	Size    int64
}

// PlanZip walks the given relative paths (files or folders) and returns the
// archive they would produce. Enumerating up front costs one directory walk
// but buys an exact Content-Length, which is the difference between a phone
// showing "12% · 2 min left" and showing nothing at all.
func (r *Root) PlanZip(rels []string) (*ZipPlan, error) {
	plan := &ZipPlan{}
	taken := map[string]bool{}

	for _, rel := range rels {
		full, err := r.Resolve(rel)
		if err != nil {
			return nil, err
		}

		info, err := os.Stat(full)
		if err != nil {
			return nil, err
		}

		if !info.IsDir() {
			plan.add(taken, path.Base(filepath.ToSlash(full)), full, info)
			continue
		}

		// A selected folder keeps its own name as the archive's top level,
		// except for the share root itself, which has nothing above it.
		prefix := ""
		if full != r.real {
			prefix = filepath.Base(full)
		}

		err = filepath.WalkDir(full, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Skips directories, symlinks (which could point outside the
			// root) and devices (which would block forever on read).
			if !d.Type().IsRegular() {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return err
			}

			relPath, err := filepath.Rel(full, p)
			if err != nil {
				return err
			}

			plan.add(taken, path.Join(prefix, filepath.ToSlash(relPath)), p, info)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	plan.Size = zipSize(plan.Entries)
	return plan, nil
}

func (p *ZipPlan) add(taken map[string]bool, name, diskPath string, info os.FileInfo) {
	name = uniqueName(taken, name)
	taken[name] = true
	p.Entries = append(p.Entries, ZipEntry{
		Name:    name,
		Path:    diskPath,
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
	})
}

// uniqueName keeps a multi-file selection from producing two members with
// the same name, which some unzippers silently resolve by dropping one.
func uniqueName(taken map[string]bool, name string) string {
	if !taken[name] {
		return name
	}

	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !taken[candidate] {
			return candidate
		}
	}
}

// Stream writes the planned archive, one file at a time, so folders much
// larger than available memory still download. Every member is written at
// exactly the size recorded while planning, keeping the stream consistent
// with the Content-Length already on the wire.
func (p *ZipPlan) Stream(w io.Writer) error {
	bw := bufio.NewWriterSize(w, bufSize)
	zw := zip.NewWriter(bw)

	for _, e := range p.Entries {
		if err := writeZipEntry(zw, e); err != nil {
			return err
		}
	}

	// Close writes the central directory; dropping its error would hand
	// the phone a truncated, unopenable archive with no trace in the log.
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalising archive: %w", err)
	}

	return bw.Flush()
}

func writeZipEntry(zw *zip.Writer, e ZipEntry) error {
	hdr := &zip.FileHeader{
		Name:     e.Name,
		Modified: e.ModTime,
		// Store, not Deflate: the payload is typically already-compressed
		// photos and video, so deflating burns CPU for ~0 size win and
		// would bottleneck the stream. It also makes the output size
		// predictable, which is what Content-Length depends on.
		Method: zip.Store,
	}
	hdr.SetMode(e.Mode)

	dst, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}

	src, err := os.Open(e.Path)
	if err != nil {
		return err
	}
	defer src.Close()

	return copyExactly(dst, src, e.Size)
}
