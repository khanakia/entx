package entpoly

import (
	_ "embed"
	"text/template"
)

// polyTmplText is the raw text of the codegen template. The //go:embed
// directive bakes the template into the package binary at build time so
// the extension has zero filesystem dependencies at runtime — users do
// not need to ship the templates/ directory alongside their build.
//
//go:embed templates/polymorphic.tmpl
var polyTmplText string

// polyTmpl is the parsed template, ready for execution. Built once at
// package-init time using text/template (not html/template) because the
// output is Go source code: html/template would helpfully escape `<`, `>`,
// and `&`, none of which is appropriate for Go.
//
// template.Must wraps the parse — if the embedded template is malformed,
// the package fails to load with a clear error rather than failing at
// every codegen invocation with the same root cause.
var polyTmpl = template.Must(template.New("polymorphic").Parse(polyTmplText))
