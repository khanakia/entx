# Internal development

This file is for **contributors hacking on enttui itself**, not end users. End users should follow the [README](../README.md) — they consume `enttui` as a library and never touch the Taskfile here.

## Taskfile

`enttui/Taskfile.yml` is purely for internal dev iteration on this repo's POC example. Run from the module directory:

```bash
cd enttui
```

| Command       | What it does                                                       |
|---------------|---------------------------------------------------------------------|
| `task cli`    | Build the codegen CLI into `../bin/enttui`                          |
| `task gen`    | Run the CLI against `../dbent/schema`, writing to `examples/aicoder/gen/` |
| `task build`  | Compile the example binary `../bin/enttui-example` (NO regen)       |
| `task run`    | `build` + launch against the demo DB. Override `DB=` / `PROJECT=`   |
| `task clean`  | Remove generated files + binaries                                   |

**`run` deliberately does not depend on `gen`.** Schema changes require an explicit `task gen` step — same separation a real user would have in their own pipeline.

Override the demo DB:

```bash
task run \
  DB=/Volumes/D/www/projects/khanakia/ai-coder/aicoder-cli-go/.aicoder/aicoder.db \
  PROJECT=prj_019e17b92e0877728319a7625cdefa42
```

Or from anywhere outside the module:

```bash
task -d enttui run
task -t enttui/Taskfile.yml run
```

## Hand-running without Task

If you don't have `task` installed:

```bash
# Build CLI
go build -o ./bin/enttui ./enttui/cmd/enttui

# Regenerate
./bin/enttui --schema ./dbent/schema --out ./enttui/examples/aicoder/gen --package enttuigen

# Build example
go build -o ./bin/enttui-example ./enttui/examples/aicoder

# Launch
./bin/enttui-example --db ./.aicoder/aicoder.db --project prj_<your_id>
```

## Demo DB locations

Two SQLite files in this workspace match `dbent/schema`:

| Path | Notes |
|------|-------|
| `.aicoder/aicoder.db` | Sparse — 4 rules, not much else. Used as the Taskfile default. |
| `/Volumes/D/www/projects/khanakia/ai-coder/aicoder-cli-go/.aicoder/aicoder.db` | Rich — 52 tasks, 26 rules, 20 memories, comments, edges populated. Use this for any UX testing of edges / drill / filter. |

Project IDs (paste into `PROJECT=`):

- Local repo DB: `prj_019e16108e537324aa99b9132d96f7d9`
- aicoder-cli-go DB: `prj_019e17b92e0877728319a7625cdefa42`

## Related

- [DEVELOPING.md](DEVELOPING.md) — code-level contributor guide.
- [CODEGEN.md](CODEGEN.md) — what the CLI / `enttui.Generate` actually does.
