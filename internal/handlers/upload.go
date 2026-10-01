// Package handlers has all the HTTP handlers
package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/ArditZubaku/go-local-image-uploader/internal/config"
	"github.com/ArditZubaku/go-local-image-uploader/internal/storage"
	"github.com/ArditZubaku/go-local-image-uploader/ui"
)

type uploadResult struct {
	SavedFiles []string `json:"savedFiles,omitempty"`
	Error      string   `json:"error,omitempty"`
}

func Register(mux *http.ServeMux, cfg config.Config) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			respond(w, r, http.StatusOK, &uploadResult{})
		case http.MethodPost:
			handleUpload(w, r, cfg)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	registerBrowse(mux, cfg)
}

func handleUpload(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	if cfg.MaxUploadSize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxUploadSize)
	}

	// Streaming reader: each part is written to disk as it arrives
	// instead of buffering the whole request (or whole files) first.
	mr, err := r.MultipartReader()
	if err != nil {
		respond(w, r, http.StatusBadRequest, &uploadResult{Error: "could not parse upload form"})
		return
	}

	saved, err := storage.SaveStream(cfg.UploadDir, mr)
	if err != nil {
		respond(w, r, http.StatusInternalServerError, &uploadResult{Error: err.Error()})
		return
	}

	respond(w, r, http.StatusOK, &uploadResult{SavedFiles: saved})
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
