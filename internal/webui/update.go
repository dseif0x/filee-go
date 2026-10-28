package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dseif0x/filee-go/internal/app"
)

// RepositoryURL is where releases are published.
const RepositoryURL = "https://github.com/dseif0x/filee-go"

// releasesAPI is the GitHub API of RepositoryURL (a var for tests).
var releasesAPI = "https://api.github.com/repos/dseif0x/filee-go/releases"

// UpdateInfo is the answer of an update check.
type UpdateInfo struct {
	Current string `json:"current"`
	// Latest is the newest release's version, "" when none was found.
	Latest string `json:"latest,omitempty"`
	Newer  bool   `json:"newer"`
	// URL is the release page; Download the file for this OS when the release has one.
	URL      string `json:"url,omitempty"`
	Download string `json:"download,omitempty"`
	// Notes are the release notes of every version after this one, newest first.
	Notes []ReleaseNote `json:"notes,omitempty"`
	// Error says why the check didn't answer (offline, no release yet).
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

// ReleaseNote is one release's tag and text.
type ReleaseNote struct {
	Version string `json:"version"`
	Body    string `json:"body"`
}

type release struct {
	Tag        string `json:"tag_name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

var (
	updateMu   sync.Mutex
	lastUpdate *UpdateInfo
)

func (s *server) checkUpdate(w http.ResponseWriter, r *http.Request) {
	updateMu.Lock()
	cached := lastUpdate
	updateMu.Unlock()
	if cached != nil && r.URL.Query().Get("force") != "1" && time.Since(cached.CheckedAt) < 10*time.Minute {
		writeJSON(w, cached)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	info := CheckForUpdate(ctx, http.DefaultClient)
	updateMu.Lock()
	lastUpdate = &info
	updateMu.Unlock()
	writeJSON(w, info)
}

// CheckForUpdate asks GitHub for the releases of filee-go. A failed check is
// not an error of Filee: Error says why and Latest stays empty.
func CheckForUpdate(ctx context.Context, client *http.Client) UpdateInfo {
	info := UpdateInfo{Current: app.Version, CheckedAt: time.Now()}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI+"?per_page=30", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Filee/"+app.Version)
	resp, err := client.Do(req)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		info.Error = fmt.Sprintf("GitHub answered %s", resp.Status)
		return info
	}
	var list []release
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		info.Error = err.Error()
		return info
	}
	var latest *release
	for i := range list {
		r := &list[i]
		if r.Draft || r.Prerelease {
			continue
		}
		if _, ok := parseVersion(r.Tag); !ok {
			continue
		}
		if latest == nil || isNewer(r.Tag, latest.Tag) {
			latest = r
		}
	}
	if latest == nil {
		info.Error = "no release yet"
		return info
	}
	info.Latest = normalizeVersion(latest.Tag)
	info.URL = latest.HTMLURL
	info.Newer = isNewer(latest.Tag, app.Version)
	for _, a := range latest.Assets {
		n := strings.ToLower(a.Name)
		if strings.Contains(n, runtime.GOOS) && (strings.Contains(n, runtime.GOARCH) || strings.Contains(n, "universal")) {
			info.Download = a.URL
			break
		}
	}
	if info.Newer {
		for _, r := range list {
			if !r.Draft && !r.Prerelease && isNewer(r.Tag, app.Version) && !isNewer(r.Tag, latest.Tag) {
				info.Notes = append(info.Notes, ReleaseNote{Version: normalizeVersion(r.Tag), Body: r.Body})
			}
		}
	}
	return info
}

// parseVersion reads "v1.2" / "1.2.0-beta+abc" as 1.2.0 (missing parts 0, suffixes ignored).
func parseVersion(text string) ([3]int, bool) {
	var v [3]int
	core := strings.TrimLeft(strings.TrimSpace(text), "vV")
	core, _, _ = strings.Cut(core, "-")
	core, _, _ = strings.Cut(core, "+")
	parts := strings.Split(core, ".")
	if core == "" || len(parts) > 4 {
		return v, false
	}
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func isNewer(tag, current string) bool {
	a, ok1 := parseVersion(tag)
	b, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func normalizeVersion(tag string) string {
	v, ok := parseVersion(tag)
	if !ok {
		return tag
	}
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}
