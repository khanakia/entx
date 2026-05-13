// Standalone GraphQL server for manual exploration of the testentpoly
// harness. Spins up an in-memory SQLite database, seeds a handful of
// rows that exercise the polymorphic union surface, and serves the
// generated gqlgen schema at :8080.
//
//	go run ./cmd/serve              # default :8080
//	PORT=9090 go run ./cmd/serve    # override port
//
// Endpoints:
//
//	POST /query        — GraphQL endpoint
//	GET  /             — Apollo Sandbox-style playground (gqlgen built-in)
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	_ "modernc.org/sqlite"

	"github.com/khanakia/entx/testentpoly/api/gql"
	"github.com/khanakia/entx/testentpoly/ent"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ctx := context.Background()
	client := mustOpen()
	defer client.Close()

	if err := client.Schema.Create(ctx); err != nil {
		log.Fatalf("schema migrate: %v", err)
	}
	ent.RegisterPolyHooks(client)
	mustSeed(ctx, client)

	srv := handler.NewDefaultServer(
		gql.NewExecutableSchema(gql.Config{Resolvers: &gql.Resolver{Client: client}}),
	)
	http.Handle("/", playground.Handler("testentpoly", "/query"))
	http.Handle("/query", srv)

	addr := ":" + port
	log.Printf("testentpoly GraphQL listening on http://localhost%s/  (playground)", addr)
	log.Printf("                       endpoint:  http://localhost%s/query", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func mustOpen() *ent.Client {
	db, err := sql.Open("sqlite", "file:testentpoly_serve?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	drv := entsql.OpenDB("sqlite3", db)
	return ent.NewClient(ent.Driver(drv))
}

// mustSeed creates a handful of parents (Post, Video, Image), each with
// a comment attached. Lets a user hit the playground and immediately
// see the polymorphic union resolve across types.
func mustSeed(ctx context.Context, client *ent.Client) {
	now := time.Now()
	p1 := client.Post.Create().SetTitle("Hello world").SetPublished(true).SetUpdatedAt(now).SaveX(ctx)
	p2 := client.Post.Create().SetTitle("Draft").SetPublished(false).SetUpdatedAt(now).SaveX(ctx)
	v1 := client.Video.Create().SetTitle("Intro screencast").SetUpdatedAt(now).SaveX(ctx)
	i1 := client.Image.Create().SetURL("https://example.com/cover.png").SetUpdatedAt(now).SaveX(ctx)

	client.Comment.Create().SetBody("First comment on post 1").SetCommentable(p1).SaveX(ctx)
	client.Comment.Create().SetBody("Another on post 1").SetCommentable(p1).SaveX(ctx)
	client.Comment.Create().SetBody("Heads up about the draft").SetCommentable(p2).SaveX(ctx)
	client.Comment.Create().SetBody("Nice video!").SetCommentable(v1).SaveX(ctx)
	client.Comment.Create().SetBody("Love the cover").SetCommentable(i1).SaveX(ctx)

	fmt.Printf("seeded: posts=2 videos=1 images=1 comments=5\n")
}
