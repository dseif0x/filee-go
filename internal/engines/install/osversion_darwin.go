package install

import "syscall"

// macOSVersion is the running macOS ("14.5"), "" when unknown.
func macOSVersion() string {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return ""
	}
	return v
}
