// Package email is the built-in "email" engine: e-mail messages (.eml, RFC
// 5322 / MIME) as a web page (the headers and the HTML body, or the plain
// text; pictures sent inline with "cid:" links become data URIs), as plain
// text, or their attachments as a ZIP. Outlook .msg files are rebuilt as
// MIME messages first and take the same path; MSG → EML writes that
// message. MHTML web archives are MIME messages too: the saved page becomes
// one HTML file with the same sanitizer.
package email

import (
	"context"
	"errors"
	"os"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/doc"
)

// Converter is the e-mail engine.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) ID() string          { return "email" }
func (*Converter) Name() string        { return "E-mail (built-in)" }
func (*Converter) MaxParallelism() int { return 0 }

// Edges: MSG has direct edges too; through EML would rebuild and parse the
// message twice. A web archive becomes its page; every HTML target follows
// from there (the DOCX and HWPX writers read it directly, one step shorter).
func (*Converter) Edges() []convert.Edge {
	return []convert.Edge{
		convert.E("eml", "html"), convert.E("eml", "txt"), convert.E("eml", "zip"),
		convert.E("msg", "eml"), convert.E("msg", "html"), convert.E("msg", "txt"), convert.E("msg", "zip"),
		convert.E("mhtml", "html"),
	}
}

func (*Converter) Status() convert.Status {
	return convert.Available("EML, MSG → HTML, TXT, ZIP (attachments); MSG → EML; MHTML → HTML", "built-in")
}

func (*Converter) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	progress.Report(0.1)
	if step.From == "mhtml" {
		m, err := loadMhtml(step.InputPath)
		if err != nil {
			return nil, err
		}
		page, err := mhtmlPage(m)
		if err != nil {
			return nil, err
		}
		progress.Report(0.8)
		out := step.Output.Allocate("html", "")
		if out == "" {
			return nil, nil
		}
		if err := os.WriteFile(out, []byte(page), 0o644); err != nil {
			return nil, err
		}
		progress.Report(1)
		return []string{out}, nil
	}
	var raw []byte
	var m *part
	var err error
	if step.From == "msg" {
		if raw, err = loadOutlookMessage(step.InputPath); err == nil {
			m, err = parseMessage(raw)
		}
	} else {
		m, err = loadMessage(step.InputPath)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress.Report(0.5)
	switch step.To {
	case "eml":
		out := step.Output.Allocate("eml", "")
		if out == "" {
			return nil, nil
		}
		if raw == nil {
			raw, _ = os.ReadFile(step.InputPath)
		}
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			return nil, err
		}
		progress.Report(1)
		return []string{out}, nil
	case "zip":
		list := attachments(m)
		if len(list) == 0 {
			return nil, errors.New("the e-mail has no attachments")
		}
		out := step.Output.Allocate("zip", "")
		if out == "" {
			return nil, nil
		}
		if err := writeAttachmentsZip(list, out); err != nil {
			return nil, err
		}
		progress.Report(1)
		return []string{out}, nil
	}
	out := step.Output.Allocate(step.To, "")
	if out == "" {
		return nil, nil
	}
	var data []byte
	if step.To == "html" {
		data = []byte(messageHTML(m))
	} else {
		data = append([]byte{0xEF, 0xBB, 0xBF}, messageText(m)...)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return nil, err
	}
	progress.Report(1)
	return []string{out}, nil
}

// MHTML is a built-in document reader: the saved page with its pictures, sanitized like an e-mail.
func init() { doc.RegisterReader("mhtml", readMhtmlDocument) }
