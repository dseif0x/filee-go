package archive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/presets"
	"github.com/dseif0x/filee-go/internal/tools"
)

// The 7-Zip console program (7zz / 7z / 7za: Filee's engines/7zip, then one
// installed on the system, e.g. Homebrew's sevenzip on macOS) lists, unpacks
// and creates the archives Filee has no reader or writer of its own for
// (7Z output, DMG, LHA, ARJ, CAB with LZX, disk images …). Its messages are
// turned into clear errors.

// SevenZipSpec finds the program.
var SevenZipSpec = tools.Spec{
	Names:   []string{"7zz", "7z", "7za"},
	Env:     "FILEE_7ZIP",
	Package: "7zip",
	Dirs: map[string][]string{
		"darwin":  {"7zip"},
		"linux":   {"7zip"},
		"windows": {"7zip", "C:/Program Files/7-Zip", "C:/Program Files (x86)/7-Zip"},
	},
}

// locateSevenZip returns the 7-Zip program, or "".
func locateSevenZip() string { return tools.Find(SevenZipSpec) }

// sevenZipTimeout: archives can be many gigabytes; the timeout only ends a process that hangs.
const sevenZipTimeout = 6 * time.Hour

// noPassword is passed when reading, so 7-Zip never waits for one on the
// console; for encrypted archives it reports "Wrong password".
const noPassword = "-pfilee-no-password"

// sevenZipEntry is one entry of a technical listing (7z l -slt).
type sevenZipEntry struct {
	Path         string
	Folder       bool
	Size         int64
	Encrypted    bool
	SymbolicLink string
	HardLink     string
	Mode         string
}

// IsSymbolicLink: links are never unpacked (they could point anywhere).
func (e sevenZipEntry) IsSymbolicLink() bool {
	if e.SymbolicLink != "" {
		return true
	}
	for _, part := range strings.Fields(e.Mode) {
		if len(part) == 10 && part[0] == 'l' {
			return true
		}
	}
	return false
}

type sevenZip struct{ exe string }

func (s *sevenZip) run(ctx context.Context, args ...string) (*tools.Result, error) {
	res, err := tools.Run(ctx, tools.Cmd{Path: s.exe, Args: args, Timeout: sevenZipTimeout})
	var exit *tools.ExitError
	if errors.As(err, &exit) {
		return res, nil // the exit code is judged by the caller
	}
	return res, err
}

func (s *sevenZip) list(ctx context.Context, archive string) ([]sevenZipEntry, error) {
	abs, _ := filepath.Abs(archive)
	res, err := s.run(ctx, "l", "-slt", "-sccUTF-8", "-bsp0", noPassword, "--", abs)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 && res.ExitCode != 1 {
		return nil, sevenZipFailure(archive, res)
	}
	return parseListing(string(res.Stdout)), nil
}

// extract unpacks everything but exclude into dir; clashing names are renamed, not overwritten.
func (s *sevenZip) extract(ctx context.Context, archive, dir string, exclude []string, workDir string) error {
	absDir, _ := filepath.Abs(dir)
	args := []string{"x", "-y", "-aou", "-sccUTF-8", "-scsUTF-8", "-bsp0", "-bb0", noPassword, "-o" + absDir}
	if len(exclude) > 0 {
		// A list file keeps unusual names intact; -spd turns off wildcards so "*" in a name is literal.
		list := filepath.Join(workDir, "exclude-"+randomSuffix()+".txt")
		if err := os.WriteFile(list, []byte(strings.Join(exclude, "\n")+"\n"), 0o644); err != nil {
			return err
		}
		defer os.Remove(list)
		args = append(args, "-spd", "-x@"+list)
	}
	abs, _ := filepath.Abs(archive)
	args = append(args, "--", abs)
	res, err := s.run(ctx, args...)
	if err != nil {
		return err
	}
	if res.ExitCode == 0 || res.ExitCode == 1 || res.ExitCode == 2 && onlyLinkErrors(res) {
		return nil
	}
	return sevenZipFailure(archive, res)
}

// add creates archive (which must not exist) from files and folders, each under its own name.
// typ is 7-Zip's archive type: 7z, zip, tar, gzip, bzip2 or xz.
func (s *sevenZip) add(ctx context.Context, archive, typ string, level presets.ArchiveLevel, items []string, workDir string) error {
	list := filepath.Join(workDir, "items-"+randomSuffix()+".txt")
	var lines []string
	for _, it := range items {
		abs, _ := filepath.Abs(it)
		lines = append(lines, abs)
	}
	if err := os.WriteFile(list, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	defer os.Remove(list)
	abs, _ := filepath.Abs(archive)
	// -ssw packs files other programs have open; -sse stops instead of silently leaving out unreadable
	// files. No "--" here: it would also turn off the @list file.
	res, err := s.run(ctx, "a", "-t"+typ, levelSwitch(level), "-ssw", "-sse", "-y", "-sccUTF-8", "-scsUTF-8", "-bsp0", "-bb0", abs, "@"+list)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 && res.ExitCode != 1 || !exists(archive) {
		return fmt.Errorf("7-Zip could not create the archive (exit %d). %s", res.ExitCode, errorText(res))
	}
	return nil
}

// levelSwitch is 7-Zip's -mx switch for a preset level.
func levelSwitch(level presets.ArchiveLevel) string {
	switch level {
	case presets.ArchiveStore:
		return "-mx0"
	case presets.ArchiveFast:
		return "-mx1"
	case presets.ArchiveMaximum:
		return "-mx9"
	}
	return "-mx5"
}

// parseListing reads the technical listing: blocks of "Key = Value" lines,
// one per entry, after a "----------" line.
func parseListing(output string) []sevenZipEntry {
	var entries []sevenZipEntry
	var fields map[string]string
	inEntries := false
	flush := func() {
		if fields == nil {
			return
		}
		path, ok := fields["Path"]
		f := fields
		fields = nil
		if !ok {
			return
		}
		attributes := f["Attributes"]
		size, _ := strconv.ParseInt(f["Size"], 10, 64)
		mode := f["Mode"]
		if mode == "" {
			mode = attributes
		}
		entries = append(entries, sevenZipEntry{
			Path:         path,
			Folder:       f["Folder"] == "+" || strings.HasPrefix(attributes, "D"),
			Size:         size,
			Encrypted:    f["Encrypted"] == "+",
			SymbolicLink: f["Symbolic Link"],
			HardLink:     f["Hard Link"],
			Mode:         mode,
		})
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if !inEntries {
			inEntries = strings.HasPrefix(line, "----------")
			continue
		}
		if line == "" {
			flush()
			continue
		}
		if i := strings.Index(line, " = "); i > 0 {
			if fields == nil {
				fields = map[string]string{}
			}
			fields[line[:i]] = line[i+3:]
		} else if strings.HasSuffix(line, " =") {
			if fields == nil {
				fields = map[string]string{}
			}
			fields[line[:len(line)-2]] = ""
		}
	}
	flush()
	return entries
}

// onlyLinkErrors: every error 7-Zip reported is about a link it did not create (we never want links).
func onlyLinkErrors(res *tools.Result) bool {
	errs := errorLines(res)
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		l := strings.ToLower(e)
		if !strings.Contains(l, "symbolic link") && !strings.Contains(l, "dangerous link") {
			return false
		}
	}
	return true
}

// sevenZipFailure turns a failed run on an archive into an error users understand.
func sevenZipFailure(archive string, res *tools.Result) error {
	name := filepath.Base(archive)
	text := strings.ToLower(string(res.Stderr) + "\n" + string(res.Stdout))
	has := func(s string) bool { return strings.Contains(text, strings.ToLower(s)) }
	switch {
	case has("Wrong password") || has("encrypted archive"):
		return errEncrypted(name)
	case has("Cannot open the file as") || has("Is not archive") || has("Can not open the file as"):
		return errNotArchive(name)
	case has("Unexpected end") || has("Data Error") || has("CRC Failed") || has("Headers Error") || has("Missing volume"):
		return errDamaged(name, errorText(res))
	}
	return fmt.Errorf("7-Zip could not read %s (exit %d). %s", name, res.ExitCode, errorText(res))
}

// errorLines are the lines of 7-Zip's output describing errors, without its banner and statistics.
func errorLines(res *tools.Result) []string {
	var out []string
	under := false
	for _, line := range strings.Split(string(res.Stderr)+"\n"+string(res.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "ERRORS:" || line == "WARNINGS:" {
			under = true
			continue
		}
		if line == "" {
			under = false
			continue
		}
		u := strings.ToUpper(line)
		if under || strings.HasPrefix(u, "ERROR") || strings.HasPrefix(u, "OPEN ERROR") || strings.HasPrefix(u, "WARNING") {
			out = append(out, line)
		}
	}
	return out
}

func errorText(res *tools.Result) string {
	seen := map[string]bool{}
	var lines []string
	for _, l := range errorLines(res) {
		if !seen[l] && len(lines) < 3 {
			seen[l] = true
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		for _, l := range strings.Split(string(res.Stderr), "\n") {
			if l = strings.TrimSpace(l); l != "" && len(lines) < 3 {
				lines = append(lines, l)
			}
		}
	}
	return strings.Join(lines, " ")
}
