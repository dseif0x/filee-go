# filee-go

A pure-Go rewrite of [Filee](https://github.com/dseif0x/Filee): drag files
while holding a modifier key, and a donut menu at the cursor converts them
(268 formats: images, PDF, documents, spreadsheets, e-books, video, audio,
archives, CAD, fonts, subtitles, 3D, geodata).

- **No cgo**: cross-compiles to every OS with `CGO_ENABLED=0`.
- Command line: `filee convert <files…> --to pdf`.
- macOS app: menu bar icon, global drag trigger, donut overlay; settings in a
  local web UI.

Work in progress. See [AGENTS.md](AGENTS.md) for the layout.
