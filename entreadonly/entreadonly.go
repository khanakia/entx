// Package entreadonly makes selected ent schemas READ-ONLY at the
// code-generation level: ent emits NO Create/Update/Delete builders or
// client write methods for an annotated type, so writes fail to
// COMPILE (not just at runtime). The schema stays a normal table —
// queries, edges, and (entgql) GraphQL are untouched.
//
// Why this design (and not ent.View / entsql.Skip / template forks):
//   - ent gates ALL write codegen on a single switch, IsView, which is
//     only set by embedding ent.View. ent.View can't carry FK edges and
//     breaks entgql's node codegen — unusable for edge-rich schemas.
//   - There is no per-type "skip mutations" knob in ent, and forking
//     ent's core create/update/delete/client templates is fragile
//     across ent releases.
//
// So entreadonly splits the problem the only robust way:
//
//  1. Extension (codegen-time): a gen.Hook reads each node's
//     annotations and, after generation, writes a manifest of the
//     annotated type names (generic — no hardcoding).
//  2. Strip (post-codegen): a deterministic AST pass keyed ONLY on
//     those type names removes the write surface from the generated
//     code. AST of generated output is stable across ent versions;
//     template forks are not.
//
// Usage:
//
//	// schema/user.go
//	func (User) Annotations() []schema.Annotation {
//	    return []schema.Annotation{ entreadonly.ReadOnly() }
//	}
//
//	// cmd/entg: entc.Extensions(..., entreadonly.NewExtension())
//	// then after `entc.Generate`: entreadonly.Strip("./gen/ent")
package entreadonly

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
	"entgo.io/ent/schema"
)

// AnnotationName is the key under which the marker appears in a
// gen.Type's Annotations map.
const AnnotationName = "EntxReadOnly"

// manifestFile is written into the gen target dir by the extension and
// read back by Strip.
const manifestFile = "entreadonly_manifest.json"

// annotation is the schema marker. It is a no-op at the schema level;
// its only job is to be present in the gen graph so the extension can
// collect the type.
type annotation struct{}

func (annotation) Name() string { return AnnotationName }

// ReadOnly returns the schema annotation that marks a schema read-only.
// Add it to a schema's Annotations() (it composes with entgql/entsql
// annotations).
func ReadOnly() schema.Annotation { return annotation{} }

// Extension is the entc extension. Register it via
// entc.Extensions(entreadonly.NewExtension()).
type Extension struct {
	entc.DefaultExtension
}

// NewExtension returns the entreadonly entc extension.
func NewExtension() *Extension { return &Extension{} }

// Hooks writes the manifest of annotated type names AFTER generation
// (so the target dir exists). Generic: any schema carrying ReadOnly().
func (*Extension) Hooks() []gen.Hook {
	return []gen.Hook{
		func(next gen.Generator) gen.Generator {
			return gen.GenerateFunc(func(g *gen.Graph) error {
				if err := next.Generate(g); err != nil {
					return err
				}
				var names []string
				for _, n := range g.Nodes {
					if _, ok := n.Annotations[AnnotationName]; ok {
						names = append(names, n.Name)
					}
				}
				b, err := json.MarshalIndent(names, "", "  ")
				if err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(g.Config.Target, manifestFile), b, 0o644)
			})
		},
	}
}

// ---- post-codegen strip ----------------------------------------------

var clientWriteMethods = map[string]bool{
	"Create": true, "CreateBulk": true, "MapCreateBulk": true,
	"Update": true, "UpdateOne": true, "UpdateOneID": true,
	"Delete": true, "DeleteOne": true, "DeleteOneID": true,
}

// builder type suffixes a read-only type must not expose.
var builderSuffixes = []string{
	"Create", "CreateBulk", "Update", "UpdateOne", "Delete", "DeleteOne",
}

// Strip reads the manifest in genDir and removes the write surface for
// every annotated type: deletes its *_create/_update/_delete.go builder
// files, removes its <T>Client write methods, neutralizes
// <T>Client.mutate (kept — referenced by the generic Client.Mutate
// switch), and removes (*<T>).Update. Keyed only on type names, so it
// is independent of ent's file-naming rules. Idempotent.
func Strip(genDir string) error {
	raw, err := os.ReadFile(filepath.Join(genDir, manifestFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no read-only types — nothing to do
		}
		return err
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return err
	}
	ro := make(map[string]bool, len(names))
	for _, n := range names {
		ro[n] = true
	}
	if len(ro) == 0 {
		return nil
	}

	entries, err := os.ReadDir(genDir)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		path := filepath.Join(genDir, e.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return err
		}

		// (a) whole builder file for a read-only type → delete.
		if t := builderFileType(f); t != "" && ro[t] {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}

		// (b) strip/neutralize method decls for read-only receivers.
		changed := false
		kept := f.Decls[:0]
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				kept = append(kept, d)
				continue
			}
			recv, ptr := recvType(fn)
			switch {
			case clientType(recv, ro) && clientWriteMethods[fn.Name.Name]:
				changed = true
				continue // drop <T>Client write method
			case clientType(recv, ro) && fn.Name.Name == "mutate":
				fn.Body = readonlyBody()
				changed = true
			case ptr && ro[recv] && fn.Name.Name == "Update":
				changed = true
				continue // drop (*<T>).Update convenience
			}
			kept = append(kept, d)
		}
		if !changed {
			continue
		}
		f.Decls = kept
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, f); err != nil {
			return err
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// builderFileType reports the read-only-relevant type T if file f
// declares a top-level `type T<suffix> struct` for a builder suffix
// (TCreate/TUpdate/TDelete/…). Returns "" otherwise.
func builderFileType(f *ast.File) string {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, sp := range gd.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok {
				continue
			}
			for _, suf := range builderSuffixes {
				if n := len(ts.Name.Name) - len(suf); n > 0 &&
					ts.Name.Name[n:] == suf {
					return ts.Name.Name[:n]
				}
			}
		}
	}
	return ""
}

func recvType(fn *ast.FuncDecl) (name string, ptr bool) {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return "", false
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		ptr = true
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name, ptr
	}
	return "", ptr
}

// clientType reports whether recv is "<T>Client" for a read-only T.
func clientType(recv string, ro map[string]bool) bool {
	const suf = "Client"
	if n := len(recv) - len(suf); n > 0 && recv[n:] == suf {
		return ro[recv[:n]]
	}
	return false
}

func readonlyBody() *ast.BlockStmt {
	const src = `package p

import "fmt"

func _() (any, error) {
	return nil, fmt.Errorf("ent: this entity is read-only (entreadonly): no mutations")
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		panic(fmt.Sprintf("entreadonly: parse neutral body: %v", err))
	}
	return f.Decls[len(f.Decls)-1].(*ast.FuncDecl).Body
}
