// Package testentpoly is the integration harness for the entpoly module.
// See SCENARIOS.md for the full coverage matrix.
package testentpoly

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/99designs/gqlgen/graphql/handler"

	"github.com/khanakia/entx/testentpoly/api/gql"
	"github.com/khanakia/entx/testentpoly/ent"

	_ "modernc.org/sqlite"
)

// openTestClient spins up a fresh in-memory SQLite database, runs the
// ent auto-migration, registers entpoly runtime hooks, and returns the
// client. Cleanup is wired via t.Cleanup. Each test gets its own
// isolated database thanks to the unique cache name.
func openTestClient(t *testing.T) *ent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", "file:testentpoly?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	drv := entsql.OpenDB("sqlite3", db)
	client := ent.NewClient(ent.Driver(drv))

	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("schema migrate: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	// Wire the entpoly runtime hooks declared on Comment.commentable
	// (Required / Touch / Cascade / SoftDelete-on-reads).
	ent.RegisterPolyHooks(client)

	return client
}

// openTracedClient is the eager-load variant: it wraps the SQL driver in
// a tracing layer so tests can count SELECTs against specific parent
// tables. The returned tracer is shared; reset it between observations
// with tracer.Reset().
func openTracedClient(t *testing.T) (*ent.Client, *queryTracer) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:testentpoly_traced?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	base := entsql.OpenDB("sqlite3", db)
	tr := &queryTracer{inner: base}
	client := ent.NewClient(ent.Driver(tr))

	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("schema migrate: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ent.RegisterPolyHooks(client)

	// Reset after migrate so the migration's DDL doesn't appear in tests.
	tr.Reset()

	return client, tr
}

// newGQLServer wires the gqlgen executable schema for the given client
// and returns a running httptest.Server. Cleanup is via t.Cleanup. The
// caller owns the client (typically returned by openTestClient or
// openTracedClient).
func newGQLServer(t *testing.T, client *ent.Client) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler.NewDefaultServer(
		gql.NewExecutableSchema(gql.Config{Resolvers: &gql.Resolver{Client: client}}),
	))
	t.Cleanup(srv.Close)
	return srv
}
