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
var browseTmpl = template.Must(template.ParseFS(templateFS, "templates/browse.html"))

// RenderUpload renders the upload form and result.
func RenderUpload(w io.Writer, data any) error {
	return uploadTmpl.ExecuteTemplate(w, "upload.html", data)
}

// RenderBrowse renders the directory browse/download page.
func RenderBrowse(w io.Writer, data any) error {
	return browseTmpl.ExecuteTemplate(w, "browse.html", data)
}
