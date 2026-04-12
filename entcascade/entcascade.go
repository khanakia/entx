// Package entcascade provides an ent codegen extension that generates
// cascade delete functions for entities annotated with entcascade.Cascade().
//
// Since foreign keys are often disabled in ent migrations (WithForeignKeys(false)),
// cascade deletes must be handled at the application level. This extension
// auto-generates those functions by inspecting schema edges.
//
// Usage in schema:
//
//	func (Chatbot) Annotations() []schema.Annotation {
//	    return []schema.Annotation{
//	        entcascade.Cascade(),
//	    }
//	}
//
// Usage in entc:
//
//	opts := []entc.Option{
//	    entc.Extensions(entcascade.NewExtension()),
//	}
//	entc.Generate("./schema", config, opts...)
//
// Generated output (cascade_delete.go):
//
//	func CascadeDeleteChatbot(ctx context.Context, client *Client, id string) error { ... }
package entcascade

import (
	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

// Extension implements entc.Extension for cascade delete generation.
type Extension struct {
	entc.DefaultExtension
}

// NewExtension creates a new cascade delete extension.
func NewExtension() *Extension {
	return &Extension{}
}

// Hooks returns gen hooks that run after normal code generation
// to produce the cascade_delete.go file.
func (e *Extension) Hooks() []gen.Hook {
	return []gen.Hook{e.hook}
}

func (e *Extension) hook(next gen.Generator) gen.Generator {
	return gen.GenerateFunc(func(g *gen.Graph) error {
		if err := next.Generate(g); err != nil {
			return err
		}
		return generate(g)
	})
}
