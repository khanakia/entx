// Package testentgqlmulti wires each generated API package to an in-memory
// SQLite-backed ent client and exposes a helper that lets tests execute GraphQL
// operations against any of the three APIs.
package testentgqlmulti

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/99designs/gqlgen/graphql/handler"

	"github.com/khanakia/entx/testentgqlmulti/api/apidash"
	"github.com/khanakia/entx/testentgqlmulti/api/apimobile"
	"github.com/khanakia/entx/testentgqlmulti/api/apipub"
	"github.com/khanakia/entx/testentgqlmulti/ent"
	"github.com/khanakia/entx/testentgqlmulti/ent/migrate"

	_ "github.com/khanakia/entx/testentgqlmulti/ent/runtime"
	_ "modernc.org/sqlite"
)

// Fixture wires a single in-memory SQLite client to all three GraphQL
// endpoints under test. Tests share the same ent.Client so data created
// via the apidash mutation surface is visible to apipub/apimobile reads.
type Fixture struct {
	Client   *ent.Client
	Dash     *httptest.Server
	Pub      *httptest.Server
	Mobile   *httptest.Server
	shutdown []func()
}

// NewFixture returns a Fixture whose servers are torn down at test end.
func NewFixture(t *testing.T) *Fixture {
	t.Helper()

	db, err := sql.Open("sqlite", "file:entmulti?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))

	if err := client.Schema.Create(context.Background(), migrate.WithForeignKeys(false)); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	dashSrv := httptest.NewServer(handler.NewDefaultServer(
		apidash.NewExecutableSchema(apidash.Config{Resolvers: &apidash.Resolver{Client: client}}),
	))
	pubSrv := httptest.NewServer(handler.NewDefaultServer(
		apipub.NewExecutableSchema(apipub.Config{Resolvers: &apipub.Resolver{Client: client}}),
	))
	mobileSrv := httptest.NewServer(handler.NewDefaultServer(
		apimobile.NewExecutableSchema(apimobile.Config{Resolvers: &apimobile.Resolver{Client: client}}),
	))

	f := &Fixture{
		Client: client,
		Dash:   dashSrv,
		Pub:    pubSrv,
		Mobile: mobileSrv,
	}
	t.Cleanup(func() {
		dashSrv.Close()
		pubSrv.Close()
		mobileSrv.Close()
		_ = client.Close()
	})
	return f
}

// Ensure net/http is imported (some test files may not otherwise use it).
var _ = http.MethodPost
