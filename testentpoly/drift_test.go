// Scenarios 15 + 16 from SCENARIOS.md: drift-linter negative tests.
// Spawn a sub-process `go run entc.go` against a temp directory whose
// schema is deliberately broken — assert non-zero exit and the
// expected error substring on stderr.
package testentpoly

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// entpolyAbsPath returns the absolute path to the entpoly module — used
// in the temp go.mod's replace directive so the sub-process compiles
// against the real, in-tree entpoly source.
func entpolyAbsPath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../entpoly")
	if err != nil {
		t.Fatalf("abs entpoly: %v", err)
	}
	return abs
}

// writeDriftHarness writes a minimal entc.go + go.mod into dir so the
// sub-process can run codegen against the supplied schema files.
func writeDriftHarness(t *testing.T, dir string, schemaFiles map[string]string) {
	t.Helper()

	abs := entpolyAbsPath(t)

	// go.mod with replace pointing at the real entpoly source.
	goMod := `module driftcheck

go 1.26.1

replace github.com/khanakia/entx/entpoly => ` + abs + `

require (
	entgo.io/ent v0.14.6
	github.com/khanakia/entx/entpoly v0.0.0-00010101000000-000000000000
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	entc := `//go:build ignore

package main

import (
	"log"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"

	"github.com/khanakia/entx/entpoly"
)

func main() {
	if err := entc.Generate("./schema", &gen.Config{
		Target:  "./ent",
		Package: "driftcheck/ent",
	}, entc.Extensions(entpoly.NewExtension())); err != nil {
		log.Fatalf("running ent codegen: %v", err)
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "entc.go"), []byte(entc), 0o644); err != nil {
		t.Fatalf("write entc.go: %v", err)
	}

	schemaDir := filepath.Join(dir, "schema")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatalf("mkdir schema: %v", err)
	}
	for name, body := range schemaFiles {
		if err := os.WriteFile(filepath.Join(schemaDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// runDrift runs `go run entc.go` in dir and returns stderr + exit error.
func runDrift(t *testing.T, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", "run", "entc.go")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// TestDriftLinter_MixedPKsRejected — scenario 15. SKIPPED: entpoly has
// no mixed-PK-type linter today. The runtime would either accept a
// string id column (fits any PK shape) or fail at codegen if the user
// also passes MixinIDType("int") together with a uuid parent. See
// Deviations.
func TestDriftLinter_MixedPKsRejected(t *testing.T) {
	t.Skip("entpoly has no mixed-PK drift linter today (verified by grepping preprocess.go). Skipping until the linter lands.")
}

// TestDriftLinter_AllowedMismatch — scenario 16. MixinAllowed + MorphTo
// AllowedTypes disagree → codegen aborts with a clear diagnostic.
func TestDriftLinter_AllowedMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns sub-process; not suitable for -short")
	}
	dir := t.TempDir()

	post := `package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Post struct{ ent.Schema }

func (Post) Fields() []ent.Field {
	return []ent.Field{field.String("title")}
}
`
	video := `package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Video struct{ ent.Schema }

func (Video) Fields() []ent.Field {
	return []ent.Field{field.String("title")}
}
`
	// Mixin allows only Post; edge claims Post AND Video. The drift
	// linter must reject before codegen completes.
	comment := `package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/khanakia/entx/entpoly"
)

type Comment struct{ ent.Schema }

func (Comment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		entpoly.MorphMixin("commentable",
			entpoly.MixinAllowed(Post.Type),
		),
	}
}

func (Comment) Fields() []ent.Field {
	return []ent.Field{field.Text("body")}
}

func (Comment) Edges() []ent.Edge {
	return []ent.Edge{
		entpoly.MorphTo("commentable", Post.Type, Video.Type),
	}
}
`
	writeDriftHarness(t, dir, map[string]string{
		"post.go":    post,
		"video.go":   video,
		"comment.go": comment,
	})

	out, err := runDrift(t, dir)
	if err == nil {
		t.Fatalf("expected non-zero exit, got success.\noutput:\n%s", out)
	}
	if !strings.Contains(out, "drifted apart") && !strings.Contains(out, "AllowedTypes") {
		t.Errorf("error should mention drift/AllowedTypes; got:\n%s", out)
	}
}
