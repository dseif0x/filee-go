//go:build !darwin

package install

func macOSVersion() string { return "" }

func nativeArch(goarch string) string { return goarch }
