package platform

import (
	"slices"
	"testing"
)

func TestNaturalOrderAndElevation(t *testing.T) {
	names := []string{"p10.png", "P2.png", "p1.png", "p02b.png"}
	slices.SortFunc(names, NaturalCompare)
	if !slices.Equal(names, []string{"p1.png", "P2.png", "p02b.png", "p10.png"}) {
		t.Fatal(names)
	}
	if IntegrityFromRid(0x3000) != IntegrityHigh || IntegrityFromRid(0x2050) != IntegrityMedium {
		t.Fatal("rid")
	}
	if !(ElevationState{IntegrityHigh, IntegrityMedium, true}).ShellCannotReachFilee() || UnknownElevation.ShellCannotReachFilee() {
		t.Fatal("elevation")
	}
	if Null.MoveToTrash(Null{}, "x") == nil || !IsInside("/a/b/c", "/a/b") || IsInside("/a/bc", "/a/b") {
		t.Fatal("helpers")
	}
}
