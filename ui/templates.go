// Package ui provides the embedded HTML templates
package ui

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/*.html
var templateFS embed.FS

var uploadTmpl = template.Must(template.ParseFS(templateFS, "templates/upload.html"))

// RenderUpload renders the upload form and result.
func RenderUpload(w io.Writer, data any) error {
	return uploadTmpl.ExecuteTemplate(w, "upload.html", data)
}
