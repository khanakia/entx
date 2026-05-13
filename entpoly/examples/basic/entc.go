//go:build ignore

package main

import (
	"log"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"

	"github.com/khanakia/entx/entpoly"
)

func main() {
	// WithMorphMap is optional — every parent type referenced from a
	// MorphTo edge auto-registers with its snake_case alias (Post →
	// "post", Video → "video"). Pass an explicit map only to override
	// the default, e.g. to keep a persisted alias stable across a Go
	// rename. We demonstrate it here so the example surfaces the
	// override mechanism.
	opts := []entc.Option{
		entc.FeatureNames("sql/modifier"),
		entc.Extensions(entpoly.NewExtension(
			entpoly.WithMorphMap(map[string]string{
				"post":  "Post",
				"video": "Video",
				"image": "Image",
			}),
			// When set, entpoly writes a .graphql fragment with the
			// union declarations for every MorphTo(...).GQL() relation.
			entpoly.WithGQLSchemaFile("./graph/polymorphic.graphql"),
		)),
	}
	if err := entc.Generate("./schema", &gen.Config{
		Target:   "./ent",
		Package:  "github.com/khanakia/entx/entpoly/examples/basic/ent",
		Features: []gen.Feature{gen.FeatureModifier},
	}, opts...); err != nil {
		log.Fatalf("running ent codegen: %v", err)
	}
}
