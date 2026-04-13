package testentgqlmulti

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Public API (apipub) — read-only subset with renamed types.
// ---------------------------------------------------------------------------

// TestApipub_SubsetFieldsOnly proves the subset type exposes only the
// annotated fields and nothing else. Querying for "email" on PublicUser must
// fail because email isn't in the apipub target's Fields list.
func TestApipub_SubsetFieldsOnly(t *testing.T) {
	f := NewFixture(t)

	doGQL(t, f.Dash.URL, `
		mutation ($in: CreateUserInput!) {
			createUser(input: $in) { id }
		}
	`, map[string]any{"in": map[string]any{
		"firstName": "Ada",
		"lastName":  "Lovelace",
		"email":     "ada@example.com",
	}}, nil)

	// Allowed fields.
	var ok struct {
		PublicUsers struct {
			Edges []struct {
				Node struct {
					FirstName string  `json:"firstName"`
					Avatar    *string `json:"avatar"`
					Status    string  `json:"status"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"publicUsers"`
	}
	doGQL(t, f.Pub.URL, `
		{ publicUsers { edges { node { firstName avatar status } } } }
	`, nil, &ok)
	if len(ok.PublicUsers.Edges) != 1 || ok.PublicUsers.Edges[0].Node.FirstName != "Ada" {
		t.Fatalf("publicUsers: %+v", ok.PublicUsers.Edges)
	}

	// Disallowed fields (email, lastName, createdAt).
	for _, bad := range []string{"email", "lastName", "createdAt"} {
		errResp := doGQLExpectError(t, f.Pub.URL, `
			{ publicUsers { edges { node { `+bad+` } } } }
		`, nil)
		if !containsAny(errResp.Errors[0].Message, bad, "Cannot query") {
			t.Fatalf("field %q: expected 'Cannot query field' error, got %+v", bad, errResp.Errors)
		}
	}
}

// TestApipub_NoMutationRoot proves the public schema exposes no Mutation
// type at all (apipub targets set Mutations=false). Any mutation request
// should be rejected at parse/validate time.
func TestApipub_NoMutationRoot(t *testing.T) {
	f := NewFixture(t)

	errResp := doGQLExpectError(t, f.Pub.URL, `
		mutation { createUser(input: {firstName: "X", lastName: "Y", email: "z@e"}) { id } }
	`, nil)
	// gqlgen responds with a validation error naming the missing root type.
	msg := errResp.Errors[0].Message
	if !containsAny(msg, "mutation", "Mutation", "not supported") {
		t.Fatalf("expected mutation rejection, got %q", msg)
	}
}

// TestApipub_TagsSameName proves an entity exported under its default name
// in two APIs resolves independently (no @goModel directive needed, no name
// collision). Tags are readable through both dashboard and public endpoints.
func TestApipub_TagsSameName(t *testing.T) {
	f := NewFixture(t)

	doGQL(t, f.Dash.URL, `
		mutation ($in: CreateTagInput!) {
			createTag(input: $in) { id }
		}
	`, map[string]any{"in": map[string]any{"name": "howto"}}, nil)

	var pub struct {
		Tags struct {
			Edges []struct {
				Node struct{ Name string `json:"name"` } `json:"node"`
			} `json:"edges"`
		} `json:"tags"`
	}
	doGQL(t, f.Pub.URL, `{ tags { edges { node { name } } } }`, nil, &pub)
	if len(pub.Tags.Edges) != 1 || pub.Tags.Edges[0].Node.Name != "howto" {
		t.Fatalf("pub tags = %+v", pub.Tags.Edges)
	}
}

// TestApipub_FiltersWithEdgePruning proves that when Post is absent from the
// public API, "hasPostsWith: [PostWhereInput!]" is dropped from UserWhereInput
// (there's no PostWhereInput in apipub to reference), but the scalar
// "hasPosts: Boolean" survives.
func TestApipub_FiltersWithEdgePruning(t *testing.T) {
	f := NewFixture(t)

	doGQL(t, f.Dash.URL, `
		mutation ($in: CreateUserInput!) {
			createUser(input: $in) { id }
		}
	`, map[string]any{"in": map[string]any{
		"firstName": "Ada",
		"lastName":  "Lovelace",
		"email":     "ada@example.com",
	}}, nil)

	// hasPosts is retained (scalar Boolean arg, doesn't need PostWhereInput).
	var ok struct {
		PublicUsers struct {
			TotalCount int `json:"totalCount"`
		} `json:"publicUsers"`
	}
	doGQL(t, f.Pub.URL, `
		query ($w: UserWhereInput) {
			publicUsers(where: $w) { totalCount }
		}
	`, map[string]any{"w": map[string]any{"hasPosts": false}}, &ok)
	if ok.PublicUsers.TotalCount != 1 {
		t.Fatalf("expected 1 user without posts, got %d", ok.PublicUsers.TotalCount)
	}

	// hasPostsWith must have been pruned — it references PostWhereInput which
	// is not present in apipub's schema.
	errResp := doGQLExpectError(t, f.Pub.URL, `
		query {
			publicUsers(where: {hasPostsWith: [{titleContains: "x"}]}) { totalCount }
		}
	`, nil)
	if !containsAny(errResp.Errors[0].Message, "hasPostsWith", "not defined", "Unknown") {
		t.Fatalf("expected rejection of hasPostsWith, got %+v", errResp.Errors)
	}
}

// TestApipub_PublicChatbotRenamed covers the @goModel binding for a subset
// type whose GraphQL name differs from its ent name.
func TestApipub_PublicChatbotRenamed(t *testing.T) {
	f := NewFixture(t)

	doGQL(t, f.Dash.URL, `
		mutation ($in: CreateChatbotInput!) {
			createChatbot(input: $in) { id }
		}
	`, map[string]any{"in": map[string]any{
		"name":        "Sparkles",
		"description": "hi",
		"apiKey":      "secret",
	}}, nil)

	var resp struct {
		PublicChatbots struct {
			Edges []struct {
				Node struct {
					Name        string  `json:"name"`
					Description *string `json:"description"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"publicChatbots"`
	}
	doGQL(t, f.Pub.URL, `
		{ publicChatbots { edges { node { name description } } } }
	`, nil, &resp)
	if len(resp.PublicChatbots.Edges) != 1 || resp.PublicChatbots.Edges[0].Node.Name != "Sparkles" {
		t.Fatalf("publicChatbots = %+v", resp.PublicChatbots.Edges)
	}

	// apiKey must NOT be part of the PublicChatbot surface.
	errResp := doGQLExpectError(t, f.Pub.URL, `
		{ publicChatbots { edges { node { apiKey } } } }
	`, nil)
	if !containsAny(errResp.Errors[0].Message, "apiKey", "Cannot query") {
		t.Fatalf("expected apiKey rejection, got %+v", errResp.Errors)
	}
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
