// Package handlers has all the HTTP handlers
package handlers

import (
	"html/template"
	"net/http"

	"github.com/ArditZubaku/go-local-image-uploader/internal/config"
	"github.com/ArditZubaku/go-local-image-uploader/internal/storage"
	"github.com/ArditZubaku/go-local-image-uploader/ui"
)

type uploadResult struct {
	SavedFiles []string
	Error      string
}

func Register(mux *http.ServeMux, cfg config.Config) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			serveForm(w, nil)
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
}

func serveForm(w http.ResponseWriter, data *uploadResult) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if data == nil {
		data = &uploadResult{}
	}
	if err := ui.RenderUpload(w, data); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("template error"))
	}
}

func handleUpload(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	// limit request size
	r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxUploadSize)

	if err := r.ParseMultipartForm(cfg.MaxUploadSize); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		serveForm(w, &uploadResult{Error: "could not parse upload form"})
		return
	}

	saved, err := storage.SaveUploadedFiles(cfg.UploadDir, r.MultipartForm)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		serveForm(w, &uploadResult{Error: template.HTMLEscapeString(err.Error())})
		return
	}

	serveForm(w, &uploadResult{
		SavedFiles: saved,
		Error:      "",
	})
}
