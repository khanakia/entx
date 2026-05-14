// Package enttui is the top-level entry point — mirrors entc's API so
// integrating enttui into an existing codegen pipeline looks identical to
// adding `entc.Generate(...)` calls.
//
// Usage in a //go:generate program:
//
//	package main
//
//	import (
//		"log"
//
//		"entgo.io/ent/entc"
//		entcgen "entgo.io/ent/entc/gen"
//
//		"enttui"
//	)
//
//	func main() {
//		if err := entc.Generate("./schema", &entcgen.Config{
//			Target:  "./gen/ent",
//			Package: "myproject/ent",
//		}); err != nil {
//			log.Fatal(err)
//		}
//
//		if err := enttui.Generate("./schema", &enttui.Config{
//			Target:  "../tui/gen",
//			Package: "tuigen",
//			EntPkg:  "myproject/ent",
//		}); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// Two calls in one main.go, one `go generate ./...` runs both passes.
package enttui

import "enttui/codegen"

// Config is the top-level codegen configuration. Mirrors entc's
// `gen.Config` in shape so the two APIs feel symmetric.
type Config struct {
	// Target is the output directory where generated `register_*.go`
	// files will be written. Same role as entc.Config.Target.
	Target string

	// Package is the Go package name declared at the top of every
	// generated file (e.g. "tuigen"). Same role as entc.Config.Package.
	Package string

	// EntPkg is the import path of the generated ent client (the package
	// that contains *ent.Client and the per-type *ent.X structs). Example:
	// "myproject/ent" or "dbent/gen/ent".
	EntPkg string

	// Skip lists ent type names to exclude from generation. Use for
	// internal / audit / migration tables you don't want browsable.
	Skip []string
}

// Option mutates a Config — same pattern as entc's option funcs. Reserved
// for future surface (Extensions, TemplateDir, Hook, etc.) so callers can
// pass options today and we can add features without breaking signatures.
type Option func(*Config)

// Skip is a convenience builder that adds names to Config.Skip.
//
//	enttui.Generate("./schema", &enttui.Config{...}, enttui.Skip("AuditLog", "QueryLog"))
func Skip(names ...string) Option {
	return func(c *Config) { c.Skip = append(c.Skip, names...) }
}

// Generate runs the enttui codegen pipeline once. Matches the shape of
// entc.Generate so adding enttui to an existing codegen main looks like
// adding any other generator pass.
//
// schemaPath is the path to your ent schema directory (the same one you
// pass to entc.Generate).
func Generate(schemaPath string, cfg *Config, opts ...Option) error {
	if cfg == nil {
		cfg = &Config{}
	}
	for _, o := range opts {
		o(cfg)
	}

	skip := make(map[string]bool, len(cfg.Skip))
	for _, n := range cfg.Skip {
		skip[n] = true
	}

	return codegen.Generate(codegen.Options{
		SchemaPath: schemaPath,
		OutDir:     cfg.Target,
		Package:    cfg.Package,
		EntPkgPath: cfg.EntPkg,
		Skip:       skip,
	})
}
