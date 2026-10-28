package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
	"github.com/dseif0x/filee-go/internal/jobs"
)

// DefaultMaxBytes is the largest file Filee saves from the web.
const DefaultMaxBytes = 2 << 30

// Downloader saves files from the web: the bytes a browser handed over, or
// the file at an address. Downloads are plain GET requests without the
// browser's cookies or sign-in: only http and https, a few redirects, a size
// cap and timeouts. Whatever arrives is named by what it really is, never
// replaces an existing file, and programs or web pages are refused.
type Downloader struct {
	client          *http.Client
	UserAgent       string
	MaxBytes        int64
	MaxRedirects    int
	ReportInterval  time.Duration
	ResponseTimeout time.Duration
	StallTimeout    time.Duration
}

// NewDownloader returns a downloader; transport may be nil (tests pass their own).
func NewDownloader(transport http.RoundTripper, userAgent string) *Downloader {
	if transport == nil {
		transport = &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         (&net.Dialer{Timeout: 20 * time.Second}).DialContext,
			TLSHandshakeTimeout: 20 * time.Second,
			ForceAttemptHTTP2:   true,
		}
	}
	return &Downloader{
		// Redirects are followed by hand: never from https down to http.
		client:    &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		UserAgent: userAgent, MaxBytes: DefaultMaxBytes, MaxRedirects: 5,
		ReportInterval: 100 * time.Millisecond, ResponseTimeout: 30 * time.Second, StallTimeout: 30 * time.Second,
	}
}

// IsWebAddress reports an http or https address, the only ones Filee downloads.
func IsWebAddress(u *url.URL) bool {
	return u != nil && u.IsAbs() && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func megabytes(b int64) string { return fmt.Sprintf("%.0f MB", float64(b)/(1024*1024)) }

// Download saves the file at u into folder and returns its path. The name
// comes from the server (Content-Disposition), else suggested, else the
// address, and its extension from the first bytes when they say what it is.
// Refusals are *convert.RefusedError (error.web_*).
func (d *Downloader) Download(ctx context.Context, u *url.URL, folder, suggested string, progress func(jobs.TransferProgress)) (string, error) {
	if !IsWebAddress(u) {
		return "", convert.Refuse("error.web_download_failed", u.Scheme)
	}
	resp, err := d.get(ctx, u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", convert.Refuse("error.web_download_failed", fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	mimeType := resp.Header.Get("Content-Type")
	if IsPageMime(mimeType) {
		return "", convert.Refuse("error.web_page", "")
	}
	if IsProgramMime(mimeType) {
		return "", convert.Refuse("error.web_blocked", "")
	}
	if resp.ContentLength > d.MaxBytes {
		return "", convert.Refuse("error.web_too_large", megabytes(resp.ContentLength))
	}
	name := FromContentDisposition(resp.Header.Get("Content-Disposition"))
	if name == "" {
		name = suggested
	}
	if name == "" {
		name = FromURL(resp.Request.URL)
	}
	if name == "" {
		name = "download"
	}
	return d.save(ctx, resp.Body, folder, name, mimeType, resp.ContentLength, progress, true)
}

func (d *Downloader) get(ctx context.Context, u *url.URL) (*http.Response, error) {
	for hop := 0; ; hop++ {
		// The response timeout covers the headers only; the body has the stall timeout.
		rctx, cancel := context.WithCancel(ctx)
		timer := time.AfterFunc(d.ResponseTimeout, cancel)
		req, err := http.NewRequestWithContext(rctx, http.MethodGet, u.String(), nil)
		if err != nil {
			timer.Stop()
			cancel()
			return nil, convert.Refuse("error.web_download_failed", err.Error())
		}
		req.Header.Set("Accept", "*/*")
		if d.UserAgent != "" {
			req.Header.Set("User-Agent", d.UserAgent)
		}
		resp, err := d.client.Do(req)
		timedOut := !timer.Stop()
		if err == nil && timedOut {
			resp.Body.Close()
			err = context.DeadlineExceeded
		}
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if timedOut {
				return nil, convert.Refuse("error.web_timeout", "")
			}
			var dns *net.DNSError
			if errors.As(err, &dns) {
				return nil, convert.Refuse("error.web_download_failed", u.Hostname())
			}
			return nil, convert.Refuse("error.web_download_failed", err.Error())
		}
		resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			if loc := resp.Header.Get("Location"); loc != "" {
				resp.Body.Close()
				next, err := u.Parse(loc)
				// Never from https down to http, and never to anything but the web.
				if err != nil || !IsWebAddress(next) || (u.Scheme == "https" && next.Scheme != "https") {
					scheme := loc
					if err == nil {
						scheme = next.Scheme + ":"
					}
					return nil, convert.Refuse("error.web_download_failed", scheme)
				}
				if hop >= d.MaxRedirects {
					return nil, convert.Refuse("error.web_download_failed", "too many redirects")
				}
				u = next
				continue
			}
		}
		return resp, nil
	}
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// Save writes r into folder as name (sanitized, renamed to what the first
// bytes are, numbered when taken) and returns the path. A file cut short by
// an error is deleted. Programs and files over MaxBytes are refused.
func (d *Downloader) Save(ctx context.Context, r io.Reader, folder, name, mimeType string, length int64, progress func(jobs.TransferProgress)) (string, error) {
	return d.save(ctx, r, folder, name, mimeType, length, progress, false)
}

// refusePages: a download; an HTML page where a file was expected is a sign-in or error page.
func (d *Downloader) save(ctx context.Context, r io.Reader, folder, name, mimeType string, length int64, progress func(jobs.TransferProgress), refusePages bool) (string, error) {
	var last time.Time
	total := int64(0)
	report := func(final bool) {
		if progress == nil || (!final && !last.IsZero() && time.Since(last) < d.ReportInterval) {
			return
		}
		last = time.Now()
		t := jobs.TransferProgress{Bytes: total}
		if length > 0 {
			t.Total = length
		}
		progress(t)
	}
	head := make([]byte, SniffLength)
	n, err := d.readFull(ctx, r, head)
	if err != nil {
		return "", err
	}
	head = head[:n]
	if LooksLikeProgram(head) {
		return "", convert.Refuse("error.web_blocked", "")
	}
	if refusePages && LooksLikePage(head) {
		return "", convert.Refuse("error.web_page", "")
	}
	name = NameFor(SanitizeName(name, ""), head, mimeType)
	if IsBlocked(name) {
		return "", convert.Refuse("error.web_blocked", "")
	}
	f, path, err := CreateUnique(folder, name)
	if err != nil {
		return "", err
	}
	err = func() error {
		defer f.Close()
		if _, err := f.Write(head); err != nil {
			return err
		}
		total = int64(len(head))
		report(false)
		buf := make([]byte, 1<<16)
		for {
			n, err := d.read(ctx, r, buf)
			if n > 0 {
				total += int64(n)
				if total > d.MaxBytes {
					return convert.Refuse("error.web_too_large", megabytes(total))
				}
				if _, werr := f.Write(buf[:n]); werr != nil {
					return werr
				}
				report(false)
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
		report(true)
		return nil
	}()
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func (d *Downloader) readFull(ctx context.Context, r io.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := d.read(ctx, r, buf[total:])
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// read reads with the stall timeout: a download may go only so long without receiving anything.
func (d *Downloader) read(ctx context.Context, r io.Reader, buf []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := r.Read(buf)
		done <- result{n, err}
	}()
	t := time.NewTimer(d.StallTimeout)
	defer t.Stop()
	select {
	case res := <-done:
		if res.err != nil && res.err != io.EOF {
			if ctx.Err() != nil {
				return res.n, ctx.Err()
			}
			var refused *convert.RefusedError
			if errors.As(res.err, &refused) {
				return res.n, res.err
			}
			return res.n, convert.Refuse("error.web_download_failed", res.err.Error())
		}
		return res.n, res.err
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-t.C:
		return 0, convert.Refuse("error.web_timeout", "")
	}
}
