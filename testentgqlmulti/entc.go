//go:build ignore

package main

import (
	"log"

	"entgo.io/contrib/entgql"
	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
	"github.com/khanakia/entx/entgqlmulti"
)

func main() {
	gqlExt, err := entgql.NewExtension(
		entgql.WithWhereInputs(true),
		// WithSchemaGenerator wires up the full monolithic AST, which is the
		// input that entgqlmulti then filters per-API.
		entgql.WithSchemaGenerator(),
		entgql.WithSchemaPath("./ent.graphql"),
		entgql.WithSchemaHook(
			entgqlmulti.New(
				entgqlmulti.WithEntPackage("github.com/khanakia/entx/testentgqlmulti/ent"),
				entgqlmulti.WithDefaultAPI("apidash"),
				entgqlmulti.WithAPIOutputPath("apidash", "./api/apidash/schema.graphql"),
				entgqlmulti.WithAPIOutputPath("apipub", "./api/apipub/schema.graphql"),
				entgqlmulti.WithAPIOutputPath("apimobile", "./api/apimobile/schema.graphql"),
			).SchemaHook(),
		),
	)
	if err != nil {
		log.Fatalf("creating entgql extension: %v", err)
	}

	config := &gen.Config{
		Target:  "./ent",
		Package: "github.com/khanakia/entx/testentgqlmulti/ent",
		Features: []gen.Feature{
			gen.FeatureExecQuery,
		},
	}

	if err := entc.Generate("./schema", config, entc.Extensions(gqlExt)); err != nil {
		log.Fatalf("running ent codegen: %v", err)
	}
}
