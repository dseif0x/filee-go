package install

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// macOSVersion is the running macOS ("14.5"), "" when unknown.
func macOSVersion() string {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return ""
	}
	return v
}

// nativeArch is the Mac's own architecture: an Intel build of Filee running
// under Rosetta 2 on Apple silicon gets the arm64 downloads, which its child
// processes run natively.
func nativeArch(goarch string) string {
	if goarch == "amd64" {
		if v, err := unix.SysctlUint32("sysctl.proc_translated"); err == nil && v == 1 {
			return "arm64"
		}
	}
	return goarch
}
