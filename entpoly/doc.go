// Package entpoly provides an ent codegen extension that adds Laravel-style
// polymorphic relationships to ent: MorphOne, MorphMany, MorphTo, MorphToMany,
// and MorphedByMany.
//
// A polymorphic relationship lets a single child entity belong to one of N
// parent types via a (id, type) discriminator pair. Example: a Comment can
// belong to a Post or a Video; an Image can belong to any entity that owns
// a featured image.
//
// # Quick start
//
// Child schema declares the morph pair (id + type columns) via MorphTo:
//
//	func (Comment) Fields() []ent.Field {
//	    return append([]ent.Field{
//	        field.Text("body"),
//	    }, entpoly.MorphTo("commentable")...)
//	}
//
//	func (Comment) Annotations() []schema.Annotation {
//	    return []schema.Annotation{
//	        entpoly.MorphChild("commentable",
//	            entpoly.AllowedTypes("post", "video"),
//	        ),
//	    }
//	}
//
// Parent schemas declare the inverse via MorphOne / MorphMany annotations:
//
//	func (Post) Annotations() []schema.Annotation {
//	    return []schema.Annotation{
//	        entpoly.MorphMany("comments", "Comment", "commentable"),
//	        entpoly.MorphOne("featured_image", "Image", "imageable"),
//	    }
//	}
//
// Many-to-many is wired through an Edge Schema pivot whose own MorphTo pair
// targets either side:
//
//	func (Tag) Annotations() []schema.Annotation {
//	    return []schema.Annotation{
//	        entpoly.MorphedByMany("posts", "Post", "Taggable", "taggable"),
//	        entpoly.MorphedByMany("videos", "Video", "Taggable", "taggable"),
//	    }
//	}
//
// # Morph map
//
// The string written into the `*_type` column is the morph key. By default it
// is the snake_case name of the ent type ("post", "video"). Register custom
// aliases via MorphMap in your entc.go to keep the column stable across
// renames:
//
//	entpoly.MorphMap(map[string]string{
//	    "post":  "Post",
//	    "video": "Video",
//	    "image": "Image",
//	})
//
// # Registering the extension
//
// In ent/entc.go:
//
//	opts := []entc.Option{
//	    entc.Extensions(entpoly.NewExtension(
//	        entpoly.WithMorphMap(map[string]string{
//	            "post":  "Post",
//	            "video": "Video",
//	        }),
//	    )),
//	}
//	entc.Generate("./schema", config, opts...)
//
// # Foreign keys
//
// Polymorphic relationships intentionally do NOT emit foreign-key constraints
// (a column cannot reference multiple tables). entpoly emits a composite index
// on (`<name>_type`, `<name>_id`) for query performance. Referential integrity
// is the application's responsibility — pair with entcascade for cascade
// deletes.
package entpoly
