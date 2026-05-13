// Scenario 17 from SCENARIOS.md: ghost-FK column suppression.
//
// The entpoly extension strips the conventional ent-generated foreign-key
// columns (e.g. `post_comments *int`) from polymorphic-child structs —
// those would carry hard FKs to one specific parent and defeat the
// polymorphism. This test asserts the *ent.Comment struct has no such
// leftover field via reflection.
package testentpoly

import (
	"reflect"
	"strings"
	"testing"

	"github.com/khanakia/entx/testentpoly/ent"
)

// TestGhostFK_NoLeftoverFields — scenario 17.
func TestGhostFK_NoLeftoverFields(t *testing.T) {
	rt := reflect.TypeOf(ent.Comment{})

	// Names that would indicate a ghost FK survived codegen. Anything
	// of the form "<Parent>Comments" or "<parent>_comments" tag is the
	// ent convention for a foreign-key column auto-emitted from the
	// parent's MorphMany("comments", ...) edge.
	parents := []string{"Post", "Video", "Image"}
	badFieldSubstrings := []string{}
	for _, p := range parents {
		badFieldSubstrings = append(badFieldSubstrings, p+"Comments")
	}
	badTagSubstrings := []string{
		"post_comments",
		"video_comments",
		"image_comments",
	}

	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)

		for _, bad := range badFieldSubstrings {
			if strings.Contains(f.Name, bad) {
				t.Errorf("ghost FK field on ent.Comment: %q (type %s)", f.Name, f.Type)
			}
		}
		// json / sql tags also reveal the column.
		tag := string(f.Tag)
		for _, bad := range badTagSubstrings {
			if strings.Contains(tag, bad) {
				t.Errorf("ghost FK column on ent.Comment: field %q has tag containing %q (tag=%q)", f.Name, bad, tag)
			}
		}
	}
}
