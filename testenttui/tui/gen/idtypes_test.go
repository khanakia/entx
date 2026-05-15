package gen

import (
	"testing"

	"github.com/google/uuid"
)

// TestParseIDTypes is white-box: it calls the per-entity parse<Name>ID
// helpers the codegen emits. Each entity in ./schema deliberately uses a
// different ID Go type, so this proves the codegen picked the right
// inbound string→ID conversion PER ENTITY (the bug the demo found).
//
// NOTE: this file is hand-written and lives alongside the generated
// register_*.go. `task clean` only deletes register_*.go, never this.
func TestParseIDTypes(t *testing.T) {
	// User — string PK (uuid-string mixin): identity conversion.
	if got, err := parseUserID("usr_abc"); err != nil || got != "usr_abc" {
		t.Fatalf("parseUserID: got %q err %v, want \"usr_abc\" nil", got, err)
	}

	// Post — int PK: strconv.Atoi path.
	if got, err := parsePostID("42"); err != nil || got != 42 {
		t.Fatalf("parsePostID: got %d err %v, want 42 nil", got, err)
	}
	if _, err := parsePostID("not-an-int"); err == nil {
		t.Fatal("parsePostID(\"not-an-int\"): want error, got nil")
	}

	// Comment — int PK as well (a SECOND int entity, distinct helper).
	if got, err := parseCommentID("7"); err != nil || got != 7 {
		t.Fatalf("parseCommentID: got %d err %v, want 7 nil", got, err)
	}

	// Tag — uuid.UUID PK: uuid.Parse path.
	u := uuid.New()
	if got, err := parseTagID(u.String()); err != nil || got != u {
		t.Fatalf("parseTagID: got %v err %v, want %v nil", got, err, u)
	}
	if _, err := parseTagID("not-a-uuid"); err == nil {
		t.Fatal("parseTagID(\"not-a-uuid\"): want error, got nil")
	}
}
