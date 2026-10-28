package email

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
)

// outMessage is a message to write as MIME (MSG → EML), laid out like
// MimeKit's BodyBuilder: text and HTML as multipart/alternative, pictures
// the HTML shows as multipart/related, attachments as multipart/mixed.
type outMessage struct {
	from        *mailbox
	to, cc, bcc []mailbox
	subject     string
	date        time.Time
	messageID   string
	text, html  string
	hasText     bool
	hasHTML     bool
	resources   []outPart
	attachments []outPart
}

type outPart struct {
	name, mediaType, contentID string
	data                       []byte
	message                    *outMessage // an attached message
}

func boundary() string {
	var b [12]byte
	rand.Read(b[:])
	return "=-" + hex.EncodeToString(b[:])
}

func formatAddresses(list []mailbox) string {
	s := make([]string, len(list))
	for i, m := range list {
		if strings.Contains(m.address, "@") {
			s[i] = (&mail.Address{Name: m.name, Address: m.address}).String()
			continue
		}
		// Exchange-only addresses (no "@") keep the name.
		name := m.name
		if name == "" {
			name = m.address
		}
		s[i] = mime.QEncoding.Encode("utf-8", name) + " <>"
	}
	return strings.Join(s, ", ")
}

// bytes serializes the message.
func (m *outMessage) bytes() []byte {
	var b bytes.Buffer
	if m.from != nil {
		b.WriteString("From: " + formatAddresses([]mailbox{*m.from}) + "\r\n")
	}
	if !m.date.IsZero() {
		b.WriteString("Date: " + m.date.Format(time.RFC1123Z) + "\r\n")
	}
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", m.subject) + "\r\n")
	for _, l := range []struct {
		name string
		list []mailbox
	}{{"To", m.to}, {"Cc", m.cc}, {"Bcc", m.bcc}} {
		if len(l.list) > 0 {
			b.WriteString(l.name + ": " + formatAddresses(l.list) + "\r\n")
		}
	}
	if m.messageID != "" {
		b.WriteString("Message-Id: <" + m.messageID + ">\r\n")
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	m.body(&b)
	return b.Bytes()
}

func textPart(b *bytes.Buffer, subtype, text string) {
	b.WriteString("Content-Type: text/" + subtype + "; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	w := quotedprintable.NewWriter(b)
	w.Write([]byte(text))
	w.Close()
	b.WriteString("\r\n")
}

func base64Lines(data []byte) string {
	s := base64.StdEncoding.EncodeToString(data)
	var sb strings.Builder
	for len(s) > 76 {
		sb.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	sb.WriteString(s + "\r\n")
	return sb.String()
}

func (p outPart) write(b *bytes.Buffer, disposition string) {
	if p.message != nil {
		b.WriteString("Content-Type: message/rfc822\r\n")
		b.WriteString("Content-Disposition: " + mime.FormatMediaType("attachment", map[string]string{"filename": p.name}) + "\r\n\r\n")
		b.Write(p.message.bytes())
		b.WriteString("\r\n")
		return
	}
	mt := p.mediaType
	if mt == "" {
		mt = "application/octet-stream"
	}
	ct := mime.FormatMediaType(mt, map[string]string{"name": p.name})
	if ct == "" {
		ct = "application/octet-stream"
	}
	b.WriteString("Content-Type: " + ct + "\r\n")
	b.WriteString("Content-Disposition: " + mime.FormatMediaType(disposition, map[string]string{"filename": p.name}) + "\r\n")
	if p.contentID != "" {
		b.WriteString("Content-Id: <" + p.contentID + ">\r\n")
	}
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n" + base64Lines(p.data))
}

func (m *outMessage) body(b *bytes.Buffer) {
	if len(m.attachments) == 0 {
		m.alternative(b)
		return
	}
	bd := boundary()
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + bd + "\"\r\n\r\n")
	b.WriteString("--" + bd + "\r\n")
	m.alternative(b)
	for _, a := range m.attachments {
		b.WriteString("--" + bd + "\r\n")
		a.write(b, "attachment")
	}
	b.WriteString("--" + bd + "--\r\n")
}

func (m *outMessage) alternative(b *bytes.Buffer) {
	switch {
	case m.hasText && m.hasHTML:
		bd := boundary()
		b.WriteString("Content-Type: multipart/alternative; boundary=\"" + bd + "\"\r\n\r\n")
		b.WriteString("--" + bd + "\r\n")
		textPart(b, "plain", m.text)
		b.WriteString("--" + bd + "\r\n")
		m.related(b)
		b.WriteString("--" + bd + "--\r\n")
	case m.hasHTML:
		m.related(b)
	default:
		textPart(b, "plain", m.text)
	}
}

func (m *outMessage) related(b *bytes.Buffer) {
	if len(m.resources) == 0 {
		textPart(b, "html", m.html)
		return
	}
	bd := boundary()
	b.WriteString("Content-Type: multipart/related; type=\"text/html\"; boundary=\"" + bd + "\"\r\n\r\n")
	b.WriteString("--" + bd + "\r\n")
	textPart(b, "html", m.html)
	for _, r := range m.resources {
		b.WriteString("--" + bd + "\r\n")
		r.write(b, "inline")
	}
	b.WriteString("--" + bd + "--\r\n")
}
