//go:build ignore

package main

import (
	"log"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

func main() {
	if err := entc.Generate("./schema", &gen.Config{
		Target:  "./ent",
		Package: "github.com/khanakia/entx/testenttui/ent",
	}); err != nil {
		log.Fatalf("running ent codegen: %v", err)
	}
}
