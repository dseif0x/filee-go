# filee-go

A Go rewrite of [Filee](https://github.com/dseif0x/Filee), the drag-and-drop
file converter. The C# original is the reference for behaviour.

## Hard rules

- **No cgo.** Everything builds with `CGO_ENABLED=0` for darwin, linux and
  windows (amd64 and arm64). CI checks this. C libraries without a Go
  equivalent run as WebAssembly inside Go (wazero, e.g. `gen2brain/webp`,
  `go-pdfium` WebAssembly mode); big programs (FFmpeg, LibreOffice, …) run as
  external processes through `internal/tools`.
- macOS native code uses `github.com/ebitengine/purego` (and its `objc`
  package), never cgo. The GUI is macOS only for now; keep the other OSes
  building (stub files with build tags).
- Dependencies must be pure Go (or ship WASM). Check before adding one.

## Layout

- `internal/formats` – the format table (`known.go`) and extension lookup.
- `internal/presets` – presets, option groups, output naming (`Resolve`).
- `internal/convert` – the engine contract (`Converter`, `Edge`, `Step`,
  optional interfaces), `Catalog` and the route `Planner`.
- `internal/tools` – finding and running external programs.
- `internal/engines/<group>` – one package per engine group. Every engine is
  listed in `internal/engines/all/all.go`.
- `cmd/filee` – the command line and the app.

## Engines

- Mirror the C# engine's edges (`Edges` in the original) and costs, so routes
  come out the same. An engine whose program/library is missing reports
  `convert.Unavailable("engine.reason.missing", …)` from `Status()`.
- Refusals the user should understand are `convert.Refuse(key, detail)`, with
  the error keys of the original (`error.*`).
- Outputs are allocated with `step.Output.Allocate(ext, suffix)`; "" means
  skip. Scratch files go to `step.WorkDir`.
- Tests: small fixtures generated in the test or kept tiny under `testdata/`.
  Tests needing an external program skip when it is not installed.

## Style

Comments say why, sparingly, like the existing code. `gofmt`, `go vet`.
