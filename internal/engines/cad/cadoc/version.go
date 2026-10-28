package cadoc

import "strings"

// Version is ACadVersion: the AutoCAD file format release. The values keep
// ACadSharp's numbering so that versions compare in release order.
type Version int16

const (
	VersionUnknown Version = -1
	MC0_0          Version = 0
	AC1_2          Version = 1
	AC1_4          Version = 2
	AC1_50         Version = 3
	AC2_10         Version = 4
	AC1002         Version = 5
	AC1003         Version = 6
	AC1004         Version = 7
	AC1006         Version = 8
	AC1009         Version = 9
	AC1012         Version = 19
	AC1014         Version = 21
	AC1015         Version = 23
	AC1018         Version = 25
	AC1021         Version = 27
	AC1024         Version = 29
	AC1027         Version = 31
	AC1032         Version = 33
)

var versionNames = map[Version]string{
	MC0_0: "MC0.0", AC1_2: "AC1.2", AC1_4: "AC1.4", AC1_50: "AC1.50", AC2_10: "AC2.10",
	AC1002: "AC1002", AC1003: "AC1003", AC1004: "AC1004", AC1006: "AC1006", AC1009: "AC1009",
	AC1012: "AC1012", AC1014: "AC1014", AC1015: "AC1015", AC1018: "AC1018", AC1021: "AC1021",
	AC1024: "AC1024", AC1027: "AC1027", AC1032: "AC1032",
}

// String returns the $ACADVER name ("AC1032"); "Unknown" for unknown versions.
func (v Version) String() string {
	if s, ok := versionNames[v]; ok {
		return s
	}
	return "Unknown"
}

// ParseVersion maps an $ACADVER value or DWG signature ("AC1032", "AC1.50") to
// a Version; VersionUnknown when it is not one.
func ParseVersion(name string) Version {
	name = strings.ToUpper(strings.TrimSpace(name))
	for v, s := range versionNames {
		if s == name {
			return v
		}
	}
	return VersionUnknown
}

// Versions lists the known versions in release order.
func Versions() []Version {
	return []Version{MC0_0, AC1_2, AC1_4, AC1_50, AC2_10, AC1002, AC1003, AC1004, AC1006, AC1009,
		AC1012, AC1014, AC1015, AC1018, AC1021, AC1024, AC1027, AC1032}
}

// MaintenanceVersion is $ACADMAINTVER as AutoCAD writes it for the version.
func (v Version) MaintenanceVersion() int16 {
	switch v {
	case AC1015:
		return 20
	case AC1018:
		return 104
	case AC1021:
		return 50
	case AC1024:
		return 226
	case AC1027:
		return 125
	case AC1032:
		return 228
	}
	return 0
}
