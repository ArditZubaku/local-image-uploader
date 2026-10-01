// Package handlers has all the HTTP handlers
package handlers

import (
	"bufio"
	"encoding/json"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/ArditZubaku/go-local-image-uploader/internal/config"
	"github.com/ArditZubaku/go-local-image-uploader/internal/storage"
	"github.com/ArditZubaku/go-local-image-uploader/ui"
)

// uploadReadBuf sits between the socket and mime/multipart, which otherwise
// reads through its own fixed 4 KiB buffer - meaning a phone sending a
// gigabyte of photos would do it in 4 KiB socket reads.
const uploadReadBuf = 1 << 20

type uploadResult struct {
	SavedFiles []storage.SavedFile `json:"savedFiles,omitempty"`
	Error      string              `json:"error,omitempty"`
}

func Register(mux *http.ServeMux, cfg config.Config) error {
	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		return err
	}

	root, err := storage.NewRoot(cfg.ShareDir)
	if err != nil {
		return err
	}

	var thumbs *storage.Thumbnailer
	if cfg.ThumbDir != "" {
		thumbs, err = storage.NewThumbnailer(cfg.ThumbDir)
		if err != nil {
			// Previews are an optimisation, not a requirement; a bad cache
			// path should degrade the gallery, not stop the server.
			log.Printf("previews disabled: %v", err)
		}
	}

	saver := storage.NewSaver(cfg.UploadDir)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, r, http.StatusOK, &uploadResult{})
	})
	mux.HandleFunc("POST /{$}", func(w http.ResponseWriter, r *http.Request) {
		handleUpload(w, r, cfg, saver)
	})
	mux.HandleFunc("POST /upload/plan", func(w http.ResponseWriter, r *http.Request) {
		handleUploadPlan(w, r, saver)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	registerBrowse(mux, root, thumbs)
	return nil
}

// handleUploadPlan answers "which of these do you still need?" before the
// browser sends anything, so re-sending a folder that is already here costs
// one small JSON round trip instead of re-transferring every byte.
func handleUploadPlan(w http.ResponseWriter, r *http.Request, saver *storage.Saver) {
	var items []storage.ManifestItem
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&items); err != nil {
		http.Error(w, "invalid manifest", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"need": saver.Needed(items)})
}

func handleUpload(w http.ResponseWriter, r *http.Request, cfg config.Config, saver *storage.Saver) {
	if cfg.MaxUploadSize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxUploadSize)
	}

	mr, err := multipartReader(r)
	if err != nil {
		respond(w, r, http.StatusBadRequest, &uploadResult{Error: "could not parse upload form"})
		return
	}

	// Streaming reader: each part is written to disk as it arrives
	// instead of buffering the whole request (or whole files) first.
	saved, err := saver.SaveStream(mr)
	if err != nil {
		// Whatever landed before the failure is still on disk and still
		// worth reporting, so the page can mark those files done and retry
		// only the rest.
		respond(w, r, http.StatusInternalServerError, &uploadResult{SavedFiles: saved, Error: err.Error()})
		return
	}

	respond(w, r, http.StatusOK, &uploadResult{SavedFiles: saved})
}

func multipartReader(r *http.Request) (*multipart.Reader, error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, http.ErrNotMultipart
	}
	boundary, ok := params["boundary"]
	if !ok {
		return nil, http.ErrMissingBoundary
	}

	return multipart.NewReader(bufio.NewReaderSize(r.Body, uploadReadBuf), boundary), nil
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func respond(w http.ResponseWriter, r *http.Request, status int, result *uploadResult) {
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(result)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := ui.RenderUpload(w, result); err != nil {
		log.Printf("render upload template: %v", err)
	}
}
