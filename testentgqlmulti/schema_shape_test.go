package testentgqlmulti

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// ---------------------------------------------------------------------------
// Structural assertions over the SDL files emitted by entgqlmulti.
// These tests parse the generated .graphql files and validate shape without
// spinning up a server — much faster than the runtime tests and precise
// about which types should and shouldn't exist in each API.
// ---------------------------------------------------------------------------

func loadSchema(t *testing.T, path string) *ast.Schema {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gqlparser.LoadSchema(&ast.Source{Name: filepath.Base(path), Input: string(raw)})
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return s
}

func mustType(t *testing.T, s *ast.Schema, name string) *ast.Definition {
	t.Helper()
	d := s.Types[name]
	if d == nil {
		t.Fatalf("expected type %q in schema", name)
	}
	return d
}

func mustNotType(t *testing.T, s *ast.Schema, name string) {
	t.Helper()
	if s.Types[name] != nil {
		t.Fatalf("unexpected type %q present in schema", name)
	}
}

func mustField(t *testing.T, d *ast.Definition, name string) *ast.FieldDefinition {
	t.Helper()
	for _, f := range d.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("expected field %q on %s, got %v", name, d.Name, fieldNames(d.Fields))
	return nil
}

func mustNotField(t *testing.T, d *ast.Definition, name string) {
	t.Helper()
	for _, f := range d.Fields {
		if f.Name == name {
			t.Fatalf("unexpected field %q on %s", name, d.Name)
		}
	}
}

func fieldNames(fs ast.FieldList) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return out
}

// ---------------------------------------------------------------------------

func TestSchemaShape_Apidash(t *testing.T) {
	s := loadSchema(t, "api/apidash/schema.graphql")

	// Full CRUD types present.
	for _, name := range []string{
		"User", "UserConnection", "UserEdge", "UserWhereInput", "UserOrder", "UserOrderField",
		"CreateUserInput", "UpdateUserInput",
		"Chatbot", "ChatbotSummary",
		"Post", "Tag",
		"Query", "Mutation",
	} {
		mustType(t, s, name)
	}

	// Secret must NOT appear in any per-API schema (no ApiConfig annotation).
	mustNotType(t, s, "Secret")

	// Chatbot keeps all its public ent fields in the dashboard API. Note that
	// api_key is `field.Sensitive()` so ent/entgql excludes it from the Chatbot
	// output type — that's independent of entgqlmulti, but it still shows up in
	// CreateChatbotInput / UpdateChatbotInput below.
	chatbot := mustType(t, s, "Chatbot")
	for _, keep := range []string{"id", "name", "avatar", "description", "status"} {
		mustField(t, chatbot, keep)
	}

	// CreateChatbotInput must include apiKey — mutations need to accept the
	// sensitive field even though it isn't surfaced on reads.
	createIn := mustType(t, s, "CreateChatbotInput")
	mustField(t, createIn, "apiKey")

	// ChatbotSummary is a subset — avatar/description stripped.
	summary := mustType(t, s, "ChatbotSummary")
	mustNotField(t, summary, "avatar")
	mustNotField(t, summary, "description")

	// ChatbotSummary uses @goModel because the type name differs from the ent name.
	if !hasGoModelDirective(summary, "github.com/khanakia/entx/testentgqlmulti/ent.Chatbot") {
		t.Fatalf("ChatbotSummary missing @goModel directive; directives=%v", summary.Directives)
	}

	// Plain Chatbot type: no @goModel needed (type name == ent name).
	if hasGoModelDirective(chatbot, "github.com/khanakia/entx/testentgqlmulti/ent.Chatbot") {
		t.Fatalf("Chatbot unexpectedly has @goModel (type name equals ent name)")
	}

	// Query exposes "chatbotSummaries" (QueryName override) in addition to "chatbots".
	q := mustType(t, s, "Query")
	mustField(t, q, "chatbots")
	mustField(t, q, "chatbotSummaries")

	// User.posts edge exists (Post is in-API).
	user := mustType(t, s, "User")
	mustField(t, user, "posts")
	// User.secrets edge was stripped because Secret isn't exported.
	mustNotField(t, user, "secrets")
}

func TestSchemaShape_Apipub(t *testing.T) {
	s := loadSchema(t, "api/apipub/schema.graphql")

	// Subset types exist.
	for _, name := range []string{
		"PublicUser", "PublicUserConnection", "PublicUserEdge",
		"PublicChatbot", "PublicChatbotConnection", "PublicChatbotEdge",
		"Tag", "TagConnection",
	} {
		mustType(t, s, name)
	}

	// No mutation root, no orderBy input types, no Post/Secret/Chatbot exposed by their ent names.
	if s.Mutation != nil {
		t.Fatalf("apipub should not have a Mutation root, got %v", s.Mutation)
	}
	for _, absent := range []string{
		"Post", "Secret",
		"UserOrder", "UserOrderField",
		"CreateUserInput", "UpdateUserInput",
	} {
		mustNotType(t, s, absent)
	}

	// PublicUser only surfaces whitelisted fields.
	pu := mustType(t, s, "PublicUser")
	for _, keep := range []string{"id", "firstName", "avatar", "status"} {
		mustField(t, pu, keep)
	}
	for _, drop := range []string{"email", "lastName", "createdAt", "posts", "secrets"} {
		mustNotField(t, pu, drop)
	}

	// UserWhereInput was copied and then pruned — hasPostsWith removed
	// because PostWhereInput is not in this API.
	uw := mustType(t, s, "UserWhereInput")
	mustNotField(t, uw, "hasPostsWith")
	mustField(t, uw, "hasPosts") // scalar Boolean predicate still valid
}

func TestSchemaShape_Apimobile(t *testing.T) {
	s := loadSchema(t, "api/apimobile/schema.graphql")

	// QueryName override: root field is "me", not "mobileUsers".
	q := mustType(t, s, "Query")
	mustField(t, q, "me")
	mustNotField(t, q, "mobileUsers")

	// MobileUser has the camelCase-specified fields resolved properly.
	mu := mustType(t, s, "MobileUser")
	for _, keep := range []string{"id", "firstName", "lastName", "createdAt"} {
		mustField(t, mu, keep)
	}
	for _, drop := range []string{"email", "avatar", "status", "posts"} {
		mustNotField(t, mu, drop)
	}

	// @goModel binds MobileUser back to ent.User.
	if !hasGoModelDirective(mu, "github.com/khanakia/entx/testentgqlmulti/ent.User") {
		t.Fatalf("MobileUser missing @goModel directive")
	}
}

// hasGoModelDirective looks up @goModel(model: "...") on a definition.
func hasGoModelDirective(d *ast.Definition, model string) bool {
	for _, dir := range d.Directives {
		if dir.Name != "goModel" {
			continue
		}
		for _, a := range dir.Arguments {
			if a.Name == "model" && strings.Trim(a.Value.Raw, `"`) == model {
				return true
			}
			if a.Name == "model" && a.Value.Raw == model {
				return true
			}
		}
	}
	return false
}
