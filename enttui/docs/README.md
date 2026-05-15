# enttui — technical docs

These docs are AI-context-friendly: each file is self-contained, links to related ones, and assumes a reader who hasn't seen the rest of the repo.

| Doc | What it covers | Audience |
|-----|----------------|----------|
| **[ARCHITECTURE.md](ARCHITECTURE.md)** | Two-layer split (runtime + codegen), data flow, type erasure boundary, what's intentionally not here. | Anyone new to enttui internals. |
| **[CODEGEN.md](CODEGEN.md)** | Pipeline stages from `entc.LoadGraph` → `EntityMeta` → templates → gofmt'd output. Example generated file. Determinism guarantees. | Modifying the generator / templates. |
| **[RUNTIME.md](RUNTIME.md)** | `runtime/` package: App, Browser, Picker, focus model, key handling, page stack, templates, theme. | Modifying UI / wiring new pane types. |
| **[CONVENTIONS.md](CONVENTIONS.md)** | The exact rules `enttui` applies when no annotations are present: entity inclusion, title/body/status detection, edges, filters. | Understanding why a given schema looks the way it does in the TUI. |
| **[ANNOTATIONS.md](ANNOTATIONS.md)** | Full annotation reference: schema-level, field-level, edge-level. Each annotation's behavior and defaults. | Customizing per-schema behavior. |
| **[EDGE-NAVIGATION.md](EDGE-NAVIGATION.md)** | How Upward vs Drill edges work, trigger-key assignment, back-stack semantics, drill ID-filter implementation. | Anything involving navigation between entities. |
| **[DEVELOPING.md](DEVELOPING.md)** | On-disk layout, workflows for adding annotations / kinds / panes, style guide, foot-guns. | Contributing to enttui itself. |

## Quick orientation

The single most important concept: **enttui has two layers.** A handwritten generic UI (`runtime/`) and a per-schema codegen layer (`codegen/`). They're independent — you can hack on one without touching the other. See [ARCHITECTURE.md](ARCHITECTURE.md).

The single most important behavior: **the generated code is the boundary between your schema and the UI.** Read `examples/aicoder/gen/register_task.go` (or whichever generator-output file is closest to what you're tracing) to understand what enttui is actually doing for one type. Everything else is plumbing.

## Where to start as a new agent

1. Read [ARCHITECTURE.md](ARCHITECTURE.md) → 5 min.
2. Open `examples/aicoder/gen/register_task.go` and read top-to-bottom → understand the generated shape.
3. Open `runtime/spec.go` → understand `EntitySpec[T]`.
4. Now pick a task. If it's a UI/keys change → [RUNTIME.md](RUNTIME.md). If it's a codegen/schema change → [CODEGEN.md](CODEGEN.md). If it's about annotation behavior → [ANNOTATIONS.md](ANNOTATIONS.md) + [CONVENTIONS.md](CONVENTIONS.md).
