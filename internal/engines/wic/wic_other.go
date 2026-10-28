//go:build !windows

package wic

import (
	"errors"

	"github.com/dseif0x/filee-go/internal/engines/images"
)

var errWindowsOnly = errors.New("JPEG XR is read with the Windows Imaging Component, on Windows only")

func codecAvailable() error { return errWindowsOnly }

func windowsVersion() string { return "" }

func read(string) (*images.Frame, error) { return nil, errWindowsOnly }
