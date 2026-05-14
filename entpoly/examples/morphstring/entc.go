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
			entpoly.WithGQLSchemaFile("./graph/polymorphic.graphql"),
		)),
	}
	if err := entc.Generate("./schema", &gen.Config{
		Target:   "./ent",
		Package:  "github.com/khanakia/entx/entpoly/examples/morphstring/ent",
		Features: []gen.Feature{gen.FeatureModifier},
	}, opts...); err != nil {
		log.Fatalf("running ent codegen: %v", err)
	}
}
