// Package data converts structured data between JSON, JSON Lines, YAML, TOML
// and XML (and CSV for tables), plus vCard contacts and iCalendar ↔ CSV,
// in-process. Every format is read into one tree and written from it, so
// structure and types carry over as far as the target can hold them (TOML has
// no null, XML no types, CSV only tables). Output is UTF-8 without BOM (CSV:
// with BOM, like the spreadsheet engine), indented by two spaces. Spreadsheets
// reach JSON and the others through CSV (XLSX → CSV → JSON), contacts and
// calendars through CSV as well. HAR network logs are read as JSON, and become
// a table of their requests.
package data

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/engines/subtitles"
)

var trees = []string{"json", "jsonl", "yaml", "toml", "xml"}

// Converter is the data engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "data" }
func (*Converter) Name() string        { return "Data (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

func (*Converter) Edges() []convert.Edge {
	var e []convert.Edge
	for _, from := range trees {
		for _, to := range trees {
			if from != to {
				e = append(e, convert.E(from, to))
			}
		}
	}
	for _, from := range trees {
		e = append(e, convert.E(from, "csv"))
	}
	for _, to := range trees {
		e = append(e, convert.E("csv", to))
	}
	e = append(e, convert.E("vcf", "csv"), convert.E("csv", "vcf"))
	for _, to := range append(append([]string{}, trees...), "csv") {
		e = append(e, convert.E("har", to))
	}
	return append(e, convert.E("ics", "csv"), convert.E("csv", "ics"))
}

func (*Converter) Status() convert.Status {
	return convert.Available("JSON, JSON Lines, YAML, TOML, XML, CSV ↔ each other; vCard and iCalendar ↔ CSV", "built-in")
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.1)
	switch {
	case step.From == "vcf":
		b, err := os.ReadFile(step.InputPath)
		if err != nil {
			return nil, err
		}
		contacts := readVCard(b)
		if len(contacts) == 0 {
			return nil, errors.New("The file has no vCard contacts.")
		}
		return writeRows(ctx, step, progress, "csv", vcardRows(contacts))
	case step.To == "vcf":
		rows, err := readCSVRows(step.InputPath)
		if err != nil {
			return nil, err
		}
		contacts, err := vcardFromRows(rows)
		if err != nil {
			return nil, err
		}
		return writeText(ctx, step, progress, "vcf", writeVCard(contacts))
	case step.From == "ics":
		b, err := os.ReadFile(step.InputPath)
		if err != nil {
			return nil, err
		}
		components := readICal(b)
		if len(components) == 0 {
			return nil, errors.New("The file has no iCalendar events or to-dos.")
		}
		return writeRows(ctx, step, progress, "csv", icalRows(components))
	case step.To == "ics":
		rows, err := readCSVRows(step.InputPath)
		if err != nil {
			return nil, err
		}
		components, err := icalFromRows(rows)
		if err != nil {
			return nil, err
		}
		if len(components) == 0 {
			return nil, errNoEvents
		}
		return writeText(ctx, step, progress, "ics", writeICal(components))
	}

	tree, err := Read(step.InputPath, step.From)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress.Report(0.6)
	// Built before a path is taken, so a tree the target can't hold leaves no empty file behind.
	if step.To == "csv" {
		var rows [][]string
		if step.From == "har" {
			rows, err = harTable(tree)
		} else {
			rows, err = csvTable(tree)
		}
		if err != nil {
			return nil, err
		}
		return writeRows(ctx, step, progress, "csv", rows)
	}
	var text string
	switch step.To {
	case "json":
		text, err = writeJSON(tree)
	case "jsonl":
		text, err = writeJSONLines(tree)
	case "yaml":
		text, err = writeYAML(tree)
	case "toml":
		text, err = writeTOML(tree)
	case "xml":
		text, err = writeXML(tree)
	default:
		return nil, fmt.Errorf("Data → %s is not supported.", step.To)
	}
	if err != nil {
		return nil, err
	}
	return writeText(ctx, step, progress, step.To, text)
}

func writeRows(ctx context.Context, step *convert.Step, progress convert.Progress, ext string, rows [][]string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := step.Output.Allocate(ext, "")
	if out == "" {
		return nil, nil
	}
	if err := writeCSVRows(rows, out); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{out}, nil
}

func writeText(ctx context.Context, step *convert.Step, progress convert.Progress, ext, text string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := step.Output.Allocate(ext, "")
	if out == "" {
		return nil, nil
	}
	if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{out}, nil
}

// Read reads a data file of a format id this engine accepts into the common tree.
func Read(path, format string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch format {
	case "xml":
		return readXML(b)
	case "csv":
		return readCSV(path)
	}
	text := subtitles.DecodeText(b)
	switch format {
	case "json", "har":
		return readJSON(text)
	case "jsonl":
		return readJSONLines(text)
	case "yaml":
		return readYAML(text)
	case "toml":
		return readTOML(text)
	}
	return nil, fmt.Errorf("%s → data is not supported.", format)
}
