// Package testenttui is the runnable demo and test bed for the enttui
// library (github.com/khanakia/entx/enttui).
//
// HOW ./tui/gen IS PRODUCED
//
// enttui is a two-stage codegen:
//
//  1. ent codegen — entc.go reads ./schema and writes ./ent (the
//     standard ent client). This is plain ent; nothing enttui-specific.
//
//  2. enttui register codegen — the enttui CLI
//     (github.com/khanakia/entx/enttui/cmd/enttui) reads ./schema again,
//     inspects each entity's enttui.* annotations + its ID Go type, and
//     writes one ./tui/gen/register_<entity>.go plus register_all.go.
//     THIS is the step that produced ./tui/gen — there is intentionally
//     no generator code inside this module; the generator lives in the
//     enttui library and is invoked as a tool.
//
// Both steps are wired as `go:generate` directives below, so:
//
//	go generate ./...      # ent + enttui, in order
//
// or, equivalently and with the rest of the workflow:
//
//	task gen               # see Taskfile.yml
//
//go:generate go run entc.go
//go:generate go run github.com/khanakia/entx/enttui/cmd/enttui --schema ./schema --out ./tui/gen --package gen --ent-pkg github.com/khanakia/entx/testenttui/ent
package testenttui
