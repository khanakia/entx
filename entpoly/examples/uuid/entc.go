//go:build ignore

package main

import (
	"log"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"

	"github.com/khanakia/entx/entpoly"
)

func main() {
	if err := entc.Generate("./schema", &gen.Config{
		Target:  "./ent",
		Package: "github.com/khanakia/entx/entpoly/examples/uuid/ent",
	}, entc.Extensions(entpoly.NewExtension())); err != nil {
		log.Fatalf("running ent codegen: %v", err)
	}
}
