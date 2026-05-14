package runtime

import (
	"bytes"
	"embed"
	"text/template"
)

//go:embed templates/*.tmpl
var tmplFS embed.FS

// templates are parsed once at init from runtime/templates/*.tmpl.
var templates = template.Must(template.ParseFS(tmplFS, "templates/*.tmpl"))

// previewField is one (label, value) pair shown above the separator.
type previewField struct {
	Key   string
	Value string
}

// previewEdge is one entry in the edges footer.
type previewEdge struct {
	Trigger string
	Display string
}

// previewData is the root context for templates/preview.tmpl.
type previewData struct {
	Fields []previewField
	Body   string
	Edges  []previewEdge
}

// renderPreview executes templates/preview.tmpl against the supplied data.
func renderPreview(data previewData) string {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "preview.tmpl", data); err != nil {
		return "template error: " + err.Error()
	}
	return buf.String()
}

// statusData feeds templates/status.tmpl.
type statusData struct {
	Display   string
	Count     string
	SortField string
	SortDir   string
	Filter    string
	Error     string
}

func renderStatus(data statusData) string {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "status.tmpl", data); err != nil {
		return "template error: " + err.Error()
	}
	return buf.String()
}
