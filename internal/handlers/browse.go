package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArditZubaku/go-local-image-uploader/internal/config"
	"github.com/ArditZubaku/go-local-image-uploader/internal/storage"
	"github.com/ArditZubaku/go-local-image-uploader/internal/utils"
	"github.com/ArditZubaku/go-local-image-uploader/ui"
)

type breadcrumb struct {
	Name string
	Rel  string
}

type browseEntry struct {
	Name        string
	IsDir       bool
	Size        string
	DownloadURL string
	OpenURL     string
	ZipURL      string
}

type browseResult struct {
	Breadcrumbs []breadcrumb
	Entries     []browseEntry
	ZipURL      string
	Error       string
}

func registerBrowse(mux *http.ServeMux, cfg config.Config) {
	mux.HandleFunc("/browse", func(w http.ResponseWriter, r *http.Request) {
		handleBrowse(w, r, cfg)
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		handleDownload(w, r, cfg)
	})
	mux.HandleFunc("/download-zip", func(w http.ResponseWriter, r *http.Request) {
		handleDownloadZip(w, r, cfg)
	})
}

func handleBrowse(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	rel := r.URL.Query().Get("path")

	entries, err := storage.ListDir(cfg.ShareDir, rel)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		renderBrowse(w, &browseResult{Error: "could not list folder"})
		return
	}

	result := &browseResult{
		Breadcrumbs: buildBreadcrumbs(rel),
		ZipURL:      downloadZipURL(rel),
	}

	for _, e := range entries {
		be := browseEntry{Name: e.Name, IsDir: e.IsDir}
		if e.IsDir {
			be.OpenURL = browseURL(e.Rel)
			be.ZipURL = downloadZipURL(e.Rel)
		} else {
			be.Size = utils.FormatBytes(e.Size)
			be.DownloadURL = downloadURL(e.Rel)
		}
		result.Entries = append(result.Entries, be)
	}

	renderBrowse(w, result)
}

func handleDownload(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	rel := r.URL.Query().Get("path")

	full, err := storage.ResolveWithinRoot(cfg.ShareDir, rel)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// No Content-Disposition override: leaving it to the browser means
	// images open inline (so iOS Safari's long-press "Save to Photos"
	// still works), while anything else downloads as usual.
	http.ServeFile(w, r, full)
}

func handleDownloadZip(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	rel := r.URL.Query().Get("path")

	full, err := storage.ResolveWithinRoot(cfg.ShareDir, rel)
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

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))

	// Headers are already flushed by the time any write below can fail
	// (e.g. the client disconnects mid-stream), so there's no status
	// code left to report an error with; just log and stop.
	if err := storage.WriteZip(w, cfg.ShareDir, rel); err != nil {
		log.Printf("zip stream error: %v", err)
	}
}

func renderBrowse(w http.ResponseWriter, result *browseResult) {
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
