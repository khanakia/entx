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
			entpoly.WithMorphMap(map[string]string{
				"post":  "Post",
				"video": "Video",
				"image": "Image",
			}),
		)),
	}
	if err := entc.Generate("./schema", &gen.Config{
		Features: []gen.Feature{gen.FeatureModifier},
	}, opts...); err != nil {
		log.Fatalf("running ent codegen: %v", err)
	}
}
