package platform

import (
	"fmt"
	"strings"
	"unicode"
)

// IntegrityLevel is a Windows mandatory integrity level, lowest first.
//
// Windows keeps a lower process (Explorer) from sending input to a higher one:
// when Filee runs as administrator, drops from Explorer are refused. The
// decision is plain logic over the levels, so it lives here; reading them is
// Windows code in the GUI.
type IntegrityLevel int

const (
	IntegrityUnknown IntegrityLevel = iota
	IntegrityUntrusted
	IntegrityLow
	IntegrityMedium
	IntegrityMediumPlus
	IntegrityHigh
	IntegritySystem
)

func (l IntegrityLevel) String() string {
	return [...]string{"Unknown", "Untrusted", "Low", "Medium", "MediumPlus", "High", "System"}[l]
}

// IntegrityFromRid maps the last sub-authority of a mandatory label SID
// (S-1-16-rid); values between the well-known ones belong to the level below.
func IntegrityFromRid(rid uint32) IntegrityLevel {
	switch {
	case rid >= 0x4000:
		return IntegritySystem
	case rid >= 0x3000:
		return IntegrityHigh
	case rid >= 0x2100:
		return IntegrityMediumPlus
	case rid >= 0x2000:
		return IntegrityMedium
	case rid >= 0x1000:
		return IntegrityLow
	}
	return IntegrityUntrusted
}

// ElevationState holds the integrity levels of this process and of the
// desktop shell (Explorer).
type ElevationState struct {
	Process  IntegrityLevel
	Shell    IntegrityLevel
	Elevated bool
}

// UnknownElevation is any OS but Windows.
var UnknownElevation = ElevationState{}

// ShellCannotReachFilee: Filee runs as administrator while Explorer doesn't,
// so Explorer can't drop files on it. Unknown levels never count.
func (e ElevationState) ShellCannotReachFilee() bool {
	return e.Process >= IntegrityHigh && e.Shell != IntegrityUnknown && e.Shell < e.Process
}

func (e ElevationState) String() string {
	if e.Process == IntegrityUnknown && e.Shell == IntegrityUnknown && !e.Elevated {
		return "integrity unknown"
	}
	el := ""
	if e.Elevated {
		el = " (elevated)"
	}
	return fmt.Sprintf("integrity %s%s, Explorer %s", e.Process, el, e.Shell)
}

// NaturalLess orders names the way people number pages and files: digit runs
// by value ("p2" < "p10"), otherwise case-insensitively.
func NaturalLess(x, y string) bool { return NaturalCompare(x, y) < 0 }

// NaturalCompare compares like NaturalLess, returning -1, 0 or 1.
func NaturalCompare(x, y string) int {
	a, b := []rune(x), []rune(y)
	i, j := 0, 0
	isDigit := func(r rune) bool { return r >= '0' && r <= '9' }
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			nx := strings.TrimLeft(string(a[si:i]), "0")
			ny := strings.TrimLeft(string(b[sj:j]), "0")
			if len(nx) != len(ny) {
				return sign(len(nx) - len(ny))
			}
			if c := strings.Compare(nx, ny); c != 0 {
				return c
			}
			continue
		}
		ca, cb := unicode.ToLower(a[i]), unicode.ToLower(b[j])
		if ca != cb {
			return sign(int(ca) - int(cb))
		}
		i++
		j++
	}
	return sign((len(a) - i) - (len(b) - j))
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
