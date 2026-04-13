package testentgqlmulti

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// graphqlRequest is the standard GraphQL-over-HTTP request body.
type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphqlResponse is the standard GraphQL-over-HTTP response.
type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphqlError  `json:"errors"`
}

type graphqlError struct {
	Message string         `json:"message"`
	Path    []any          `json:"path"`
	Extras  map[string]any `json:"extensions"`
}

// doGQL executes a GraphQL operation against the given URL and decodes the
// "data" portion into `out`. Any errors returned by the server fail the test.
func doGQL(t *testing.T, url, query string, vars map[string]any, out any) *graphqlResponse {
	t.Helper()

	body, err := json.Marshal(graphqlRequest{Query: query, Variables: vars})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, string(raw))
	}
	var gr graphqlResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		t.Fatalf("decode response: %v body=%s", err, string(raw))
	}
	if len(gr.Errors) > 0 {
		t.Fatalf("graphql errors: %+v body=%s", gr.Errors, string(raw))
	}
	if out != nil && len(gr.Data) > 0 {
		if err := json.Unmarshal(gr.Data, out); err != nil {
			t.Fatalf("decode data: %v body=%s", err, string(raw))
		}
	}
	return &gr
}

// doGQLExpectError runs a GraphQL operation that is expected to fail. It
// returns the raw response so callers can assert on the error contents
// (e.g. "Cannot query field X on Y").
func doGQLExpectError(t *testing.T, url, query string, vars map[string]any) *graphqlResponse {
	t.Helper()

	body, err := json.Marshal(graphqlRequest{Query: query, Variables: vars})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var gr graphqlResponse
	_ = json.Unmarshal(raw, &gr)
	if len(gr.Errors) == 0 {
		t.Fatalf("expected errors, got data=%s", string(raw))
	}
	return &gr
}
