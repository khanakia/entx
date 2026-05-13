//go:build ignore

package main

import (
	"log"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"

	"github.com/khanakia/entx/entpoly"
)

func main() {
	opts := []entc.Option{
		entc.FeatureNames("sql/modifier"),
		entc.Extensions(entpoly.NewExtension(
			// WithMorphMap omitted intentionally: entpoly's codegen
			// currently emits one MorphKey constant per map entry and
			// produces duplicate identifiers when multiple aliases map
			// to the same schema (e.g. both "post" and "legacy_post" →
			// "Post"). Scenario 12 (TestMorphMap_Rename) is skipped
			// until entpoly merges or otherwise dedups alias emission.
			entpoly.WithGQLSchemaFile("./api/gql/polymorphic.graphql"),
		)),
	}
	if err := entc.Generate("./schema", &gen.Config{
		Target:   "./ent",
		Package:  "github.com/khanakia/entx/testentpoly/ent",
		Features: []gen.Feature{gen.FeatureModifier},
	}, opts...); err != nil {
		log.Fatalf("running ent codegen: %v", err)
	}
}
