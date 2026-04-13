package testentgqlmulti

import "testing"

// ---------------------------------------------------------------------------
// Mobile API (apimobile) — QueryName override, camelCase Fields input.
// ---------------------------------------------------------------------------

// TestApimobile_QueryNameOverride proves the root query field is named "me"
// (via QueryName override), not the default "mobileUsers".
func TestApimobile_QueryNameOverride(t *testing.T) {
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

	var resp struct {
		Me struct {
			TotalCount int `json:"totalCount"`
			Edges      []struct {
				Node struct {
					FirstName string `json:"firstName"`
					LastName  string `json:"lastName"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"me"`
	}
	doGQL(t, f.Mobile.URL, `
		{ me { totalCount edges { node { firstName lastName } } } }
	`, nil, &resp)
	if resp.Me.TotalCount != 1 {
		t.Fatalf("me.totalCount = %d, want 1", resp.Me.TotalCount)
	}
	if resp.Me.Edges[0].Node.FirstName != "Ada" || resp.Me.Edges[0].Node.LastName != "Lovelace" {
		t.Fatalf("me edge = %+v", resp.Me.Edges[0])
	}

	// Default name "mobileUsers" must NOT exist.
	errResp := doGQLExpectError(t, f.Mobile.URL, `{ mobileUsers { totalCount } }`, nil)
	if !containsAny(errResp.Errors[0].Message, "mobileUsers", "Cannot query") {
		t.Fatalf("mobileUsers should not exist, got %+v", errResp.Errors)
	}
}

// TestApimobile_CamelCaseFieldsInput proves that camelCase entries in an
// ApiTarget's Fields list ("firstName", "createdAt") are normalized
// correctly and exposed on the resulting GraphQL type. Prior to the
// snake→camel normalization fix the camelCase entries were lowercased
// and dropped.
func TestApimobile_CamelCaseFieldsInput(t *testing.T) {
	f := NewFixture(t)

	doGQL(t, f.Dash.URL, `
		mutation ($in: CreateUserInput!) {
			createUser(input: $in) { id }
		}
	`, map[string]any{"in": map[string]any{
		"firstName": "Ada",
		"lastName":  "L",
		"email":     "ada@example.com",
	}}, nil)

	// createdAt must be present — specified as camelCase in the target.
	var resp struct {
		Me struct {
			Edges []struct {
				Node struct {
					FirstName string `json:"firstName"`
					LastName  string `json:"lastName"`
					CreatedAt string `json:"createdAt"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"me"`
	}
	doGQL(t, f.Mobile.URL, `
		{ me { edges { node { firstName lastName createdAt } } } }
	`, nil, &resp)
	if resp.Me.Edges[0].Node.CreatedAt == "" {
		t.Fatal("createdAt empty; expected a populated time string")
	}
}

// TestApimobile_NoFiltersOrOrderBy proves that OrderBy/Filters args absent
// when neither flag is set in the ApiTarget.
func TestApimobile_NoFiltersOrOrderBy(t *testing.T) {
	f := NewFixture(t)

	for _, bad := range []string{
		`{ me(where: {firstName: "Ada"}) { totalCount } }`,
		`{ me(orderBy: [{direction: ASC, field: CREATED_AT}]) { totalCount } }`,
	} {
		errResp := doGQLExpectError(t, f.Mobile.URL, bad, nil)
		if !containsAny(errResp.Errors[0].Message, "Unknown argument", "not defined") {
			t.Fatalf("expected arg rejection, got %+v", errResp.Errors)
		}
	}
}
