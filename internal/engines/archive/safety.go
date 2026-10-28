package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Guards for unpacking archives from anywhere: entries that would land
// outside the target folder ("zip slip"), symbolic links, encrypted content
// and archives that would not fit on the disk; also their error messages.

// MaxEntries: more entries are refused; real archives stay far below, "entry bombs" do not.
const MaxEntries = 1_000_000

// reserve is the space that must stay free on the drive after unpacking.
const reserve = 64 << 20

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// IsUnsafePath: an entry path that is absolute, starts with a drive, or climbs out with "..".
func IsUnsafePath(p string) bool {
	if p == "" {
		return false
	}
	if p[0] == '/' || p[0] == '\\' {
		return true
	}
	if len(p) >= 2 && p[1] == ':' && (p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z') {
		return true
	}
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

// entryInfo is what the checks need to know of an entry.
type entryInfo struct {
	Path      string
	Folder    bool
	Size      int64
	Encrypted bool
	Symlink   bool
	HardLink  string
}

// checkListing refuses a listing with encrypted entries, entries or hard
// links pointing outside the target folder, too many entries, or more data
// than the drive of target can hold.
func checkListing(entries []entryInfo, archiveName, target string) error {
	if len(entries) > MaxEntries {
		return tooManyEntries(archiveName)
	}
	var total int64
	for _, e := range entries {
		if e.Encrypted {
			return errEncrypted(archiveName)
		}
		if IsUnsafePath(e.Path) || e.HardLink != "" && IsUnsafePath(e.HardLink) {
			return errUnsafe(archiveName, e.Path)
		}
		if !e.Folder && !e.Symlink {
			total += max(0, e.Size)
		}
	}
	return ensureRoom(total, target, archiveName)
}

func tooManyEntries(archiveName string) error {
	return fmt.Errorf("%s has more than %s entries; Filee does not unpack it", archiveName, groupDigits(MaxEntries))
}

// ensureRoom fails when bytes would not fit on the drive of directory.
func ensureRoom(bytes int64, directory, archiveName string) error {
	if bytes <= 0 {
		return nil
	}
	free, ok := freeSpace(directory)
	if !ok {
		return nil // network shares and the like: the write itself fails if space runs out
	}
	if bytes > free-reserve {
		return fmt.Errorf("%s unpacks to %s, but only %s is free on %s", archiveName, FormatSize(bytes), FormatSize(free), volumeName(directory))
	}
	return nil
}

func volumeName(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	if v := filepath.VolumeName(abs); v != "" {
		return v + string(filepath.Separator)
	}
	return abs
}

// resolveEntryPath turns an entry name of a built-in reader into a path
// inside root: separators normalised, characters Windows does not allow
// replaced, anything escaping root refused.
func resolveEntryPath(root, entryName, archiveName string) (string, error) {
	if IsUnsafePath(entryName) {
		return "", errUnsafe(archiveName, entryName)
	}
	var parts []string
	for _, part := range strings.FieldsFunc(entryName, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == "." {
			continue
		}
		parts = append(parts, sanitizeSegment(part))
	}
	if len(parts) == 0 {
		return "", errDamaged(archiveName, "an entry has no name")
	}
	fullRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(append([]string{fullRoot}, parts...)...)
	if !strings.HasPrefix(strings.ToLower(full), strings.ToLower(strings.TrimRight(fullRoot, `/\`)+string(filepath.Separator))) {
		return "", errUnsafe(archiveName, entryName)
	}
	return full, nil
}

// sanitizeSegment makes one path segment valid on Windows: invalid characters
// become "_", device names get a prefix.
func sanitizeSegment(segment string) string {
	var sb strings.Builder
	for _, r := range segment {
		if strings.ContainsRune(`<>:"/\|?*`, r) || unicode.IsControl(r) {
			sb.WriteByte('_')
		} else {
			sb.WriteRune(r)
		}
	}
	name := strings.TrimRight(sb.String(), ". ")
	if name == "" {
		return "_"
	}
	stem, _, _ := strings.Cut(name, ".")
	if reservedNames[strings.ToUpper(stem)] {
		return "_" + name
	}
	return name
}

// removeLinks deletes symbolic links (and junctions) below root without
// following them, so nothing packed or moved later can reach outside.
func removeLinks(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info) {
			if err := os.Remove(p); err != nil {
				return err
			}
			continue
		}
		if info.IsDir() {
			if err := removeLinks(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func errEncrypted(archiveName string) error {
	return fmt.Errorf("%s is password-protected. Filee cannot unpack encrypted archives", archiveName)
}

func errUnsafe(archiveName, entry string) error {
	return fmt.Errorf("%s was not unpacked: the entry %q points outside the target folder", archiveName, entry)
}

func errDamaged(archiveName, detail string) error {
	return fmt.Errorf("%s is damaged or incomplete (%s)", archiveName, detail)
}

func errNotArchive(archiveName string) error {
	return fmt.Errorf("%s is not a valid archive, or not the kind its extension says", archiveName)
}

var errNo7Zip = errors.New("7-Zip was not found (engines/7zip)")

// FormatSize is "1.4 GB" style size for messages.
func FormatSize(bytes int64) string {
	units := []string{"bytes", "KB", "MB", "GB", "TB"}
	value := float64(bytes)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d bytes", bytes)
	}
	if value < 10 {
		return fmt.Sprintf("%.1f %s", value, units[unit])
	}
	return fmt.Sprintf("%.0f %s", value, units[unit])
}

func groupDigits(n int) string {
	s := fmt.Sprint(n)
	var sb strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

