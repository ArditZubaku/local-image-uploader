package handlers

import (
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/ArditZubaku/go-local-image-uploader/internal/storage"
	"github.com/ArditZubaku/go-local-image-uploader/internal/utils"
	"github.com/ArditZubaku/go-local-image-uploader/ui"
)

// thumbMaxDim is the longest side of a generated preview. 480 px covers a
// 2x phone grid cell without the page pulling full-resolution originals.
const thumbMaxDim = 480

// maxListEntries caps how many rows one page renders. A camera roll with
// thousands of files would otherwise produce a multi-megabyte page that a
// phone spends longer laying out than it would spend downloading.
const maxListEntries = 1500

func init() {
	// Go's built-in table has no entry for the formats phones actually
	// produce, so without these iOS gets application/octet-stream and
	// refuses to preview or save the file to Photos.
	for ext, typ := range map[string]string{
		".heic": "image/heic",
		".heif": "image/heif",
		".avif": "image/avif",
		".webp": "image/webp",
		".jxl":  "image/jxl",
		".dng":  "image/x-adobe-dng",
		".mov":  "video/quicktime",
		".m4v":  "video/x-m4v",
		".mkv":  "video/x-matroska",
	} {
		_ = mime.AddExtensionType(ext, typ)
	}
}

type breadcrumb struct {
	Name string
	Rel  string
}

type browseEntry struct {
	Name        string
	Rel         string
	IsDir       bool
	Size        string
	DownloadURL string
	OpenURL     string
	ZipURL      string
	ThumbURL    string
}

type browseResult struct {
	Breadcrumbs []breadcrumb
	Entries     []browseEntry
	ZipURL      string
	TotalSize   string
	HasImages   bool
	Truncated   string
	Error       string
}

type browser struct {
	root   *storage.Root
	thumbs *storage.Thumbnailer
}

func registerBrowse(mux *http.ServeMux, root *storage.Root, thumbs *storage.Thumbnailer) {
	b := &browser{root: root, thumbs: thumbs}

	mux.HandleFunc("GET /browse", b.list)
	mux.HandleFunc("GET /download", b.download)
	mux.HandleFunc("GET /thumb", b.thumb)
	mux.HandleFunc("GET /download-zip", b.zipFolder)
	mux.HandleFunc("HEAD /download-zip", b.zipFolder)
	mux.HandleFunc("POST /download-zip", b.zipSelection)
}

func (b *browser) list(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")

	entries, total, err := b.root.ListDir(rel, maxListEntries)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		b.render(w, &browseResult{Error: "could not list folder"})
		return
	}

	result := &browseResult{
		Breadcrumbs: buildBreadcrumbs(rel),
		ZipURL:      downloadZipURL(rel),
		Entries:     make([]browseEntry, 0, len(entries)),
	}

	var totalBytes int64
	for _, e := range entries {
		be := browseEntry{Name: e.Name, Rel: e.Rel, IsDir: e.IsDir}
		if e.IsDir {
			be.OpenURL = browseURL(e.Rel)
			be.ZipURL = downloadZipURL(e.Rel)
		} else {
			totalBytes += e.Size
			be.Size = utils.FormatBytes(e.Size)
			be.DownloadURL = downloadURL(e.Rel)
			if b.thumbs != nil && storage.CanThumbnail(e.Name) {
				be.ThumbURL = thumbURL(e.Rel)
				result.HasImages = true
			}
		}
		result.Entries = append(result.Entries, be)
	}

	if totalBytes > 0 {
		result.TotalSize = utils.FormatBytes(totalBytes)
	}
	if total > len(entries) {
		// The folder zip still covers everything, so say so rather than
		// leaving the hidden files looking lost.
		result.Truncated = fmt.Sprintf("Showing %d of %d items. The folder .zip still includes all of them.", len(entries), total)
	}

	b.render(w, result)
}

func (b *browser) download(w http.ResponseWriter, r *http.Request) {
	full, err := b.root.Resolve(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Revalidation is cheap and correct here: ServeFile answers a
	// conditional request with a 304, so a phone re-opening the gallery
	// re-downloads nothing it already holds.
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")

	// No Content-Disposition override: leaving it to the browser means
	// images open inline (so iOS Safari's long-press "Save to Photos"
	// still works), while anything else downloads as usual.
	http.ServeFile(w, r, full)
}

func (b *browser) thumb(w http.ResponseWriter, r *http.Request) {
	if b.thumbs == nil {
		http.Error(w, "previews disabled", http.StatusNotFound)
		return
	}

	full, err := b.root.Resolve(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	cached, err := b.thumbs.Thumb(full, thumbMaxDim)
	if err != nil {
		// An unreadable or exotic image is not an error worth a 500: the
		// page just falls back to showing a plain file row.
		http.Error(w, "no preview", http.StatusNotFound)
		return
	}

	// The cache key already covers the source file's mtime and size, so a
	// preview URL never changes meaning and the phone can keep it.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, cached)
}

func (b *browser) zipFolder(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")

	full, err := b.root.Resolve(rel)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(full)
	if err != nil || !info.IsDir() {
		http.Error(w, "folder not found", http.StatusNotFound)
		return
	}

	name := filepath.Base(full)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "download"
	}

	b.streamZip(w, r, []string{rel}, name)
}

func (b *browser) zipSelection(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid selection", http.StatusBadRequest)
		return
	}

	rels := r.PostForm["path"]
	if len(rels) == 0 {
		http.Error(w, "nothing selected", http.StatusBadRequest)
		return
	}

	name := "selection"
	if len(rels) == 1 {
		name = strings.TrimSuffix(filepath.Base(rels[0]), filepath.Ext(rels[0]))
	}

	b.streamZip(w, r, rels, name)
}

func (b *browser) streamZip(w http.ResponseWriter, r *http.Request, rels []string, name string) {
	// Walking first costs one pass over the directory metadata and buys an
	// exact Content-Length, which is what turns the phone's download from
	// an open-ended spinner into a progress bar with an ETA.
	plan, err := b.root.PlanZip(rels)
	if err != nil {
		http.Error(w, "could not read selection", http.StatusNotFound)
		return
	}
	if len(plan.Entries) == 0 {
		http.Error(w, "nothing to download", http.StatusNotFound)
		return
	}

	etag := plan.ETag()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition(name+".zip"))
	w.Header().Set("ETag", etag)
	w.Header().Set("Accept-Ranges", "bytes")

	start, end, status := resolveRange(r, plan.Size, etag)
	switch status {
	case http.StatusRequestedRangeNotSatisfiable:
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", plan.Size))
		http.Error(w, "range not satisfiable", status)
		return
	case http.StatusPartialContent:
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, plan.Size))
	}

	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(status)

	if r.Method == http.MethodHead {
		return
	}

	if err := plan.StreamRange(w, start, end); err != nil {
		// Headers are already flushed by the time any write below can fail,
		// so there's no status code left to report an error with. A phone
		// tapping cancel is the ordinary way for this to end, and saying so
		// at error level would make every cancelled download look like a
		// fault.
		if clientGone(r, err) {
			return
		}
		log.Printf("zip stream error (%d files, %s): %v", len(plan.Entries), utils.FormatBytes(plan.Size), err)
	}
}

// resolveRange interprets a Range header against an archive of the given
// size, returning the byte span to send and the status to send it with.
//
// A resume is only honoured when the archive is provably unchanged: the
// plan's tag covers every input to the output bytes, so a mismatched
// If-Range (a file added, removed, or touched since) quietly falls back to
// sending the whole thing rather than splicing two different archives.
func resolveRange(r *http.Request, size int64, etag string) (start, end int64, status int) {
	header := r.Header.Get("Range")
	if header == "" {
		return 0, size - 1, http.StatusOK
	}
	if ifRange := r.Header.Get("If-Range"); ifRange != "" && ifRange != etag {
		return 0, size - 1, http.StatusOK
	}

	spec, ok := strings.CutPrefix(header, "bytes=")
	// Multiple ranges would need a multipart response for an archive that
	// has to be regenerated per span; answering with the whole file is a
	// legal and far cheaper reply.
	if !ok || strings.Contains(spec, ",") {
		return 0, size - 1, http.StatusOK
	}

	from, to, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, size - 1, http.StatusOK
	}

	switch {
	case from == "":
		// "bytes=-N": the final N bytes.
		n, err := strconv.ParseInt(to, 10, 64)
		if err != nil || n <= 0 {
			return 0, size - 1, http.StatusOK
		}
		start, end = max(0, size-n), size-1
	default:
		var err error
		if start, err = strconv.ParseInt(from, 10, 64); err != nil || start < 0 {
			return 0, size - 1, http.StatusOK
		}
		end = size - 1
		if to != "" {
			if end, err = strconv.ParseInt(to, 10, 64); err != nil {
				return 0, size - 1, http.StatusOK
			}
			end = min(end, size-1)
		}
	}

	if start >= size || end < start {
		return 0, 0, http.StatusRequestedRangeNotSatisfiable
	}

	return start, end, http.StatusPartialContent
}

// clientGone reports whether a stream ended because the phone went away -
// cancelled, backgrounded, or off the network - rather than because
// anything went wrong on this end.
func clientGone(r *http.Request, err error) bool {
	return r.Context().Err() != nil ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, net.ErrClosed)
}

// contentDisposition emits both the plain and RFC 5987 filename forms, so
// a folder named with quotes or non-ASCII characters cannot corrupt the
// header or arrive on the phone as a mangled name.
func contentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)

	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(name))
}

func (b *browser) render(w http.ResponseWriter, result *browseResult) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ui.RenderBrowse(w, result); err != nil {
		log.Printf("render browse template: %v", err)
	}
}

func buildBreadcrumbs(rel string) []breadcrumb {
	crumbs := []breadcrumb{{Name: "Home", Rel: ""}}

	rel = strings.Trim(rel, "/")
	if rel == "" {
		return crumbs
	}

	acc := ""
	for _, part := range strings.Split(rel, "/") {
		if part == "" {
			continue
		}
		if acc == "" {
			acc = part
		} else {
			acc = acc + "/" + part
		}
		crumbs = append(crumbs, breadcrumb{Name: part, Rel: acc})
	}

	return crumbs
}

func browseURL(rel string) string {
	return "/browse?" + url.Values{"path": {rel}}.Encode()
}

func downloadURL(rel string) string {
	return "/download?" + url.Values{"path": {rel}}.Encode()
}

func downloadZipURL(rel string) string {
	return "/download-zip?" + url.Values{"path": {rel}}.Encode()
}

func thumbURL(rel string) string {
	return "/thumb?" + url.Values{"path": {rel}}.Encode()
}
