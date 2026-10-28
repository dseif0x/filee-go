//go:build darwin

package ns

import (
	"image"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Core Foundation, Core Graphics and Application Services functions.
var (
	CFRelease                  func(uintptr)
	CFRetain                   func(uintptr) uintptr
	CFArrayGetCount            func(uintptr) int
	CFArrayGetValueAtIndex     func(uintptr, int) uintptr
	CFDictionaryGetValue       func(dict, key uintptr) uintptr
	cfStringCreateWithCString  func(alloc uintptr, s string, encoding uint32) uintptr
	CFStringGetCString         func(s uintptr, buf *byte, size int, encoding uint32) bool
	CFNumberGetValue           func(n uintptr, typ int, out unsafe.Pointer) bool
	CFDictionaryCreate         func(alloc uintptr, keys, values *uintptr, n int, keyCB, valueCB uintptr) uintptr
	CFLocaleCopyPreferredLangs func() uintptr
	CFRunLoopGetCurrent        func() uintptr
	CFRunLoopGetMain           func() uintptr
	CFRunLoopRun               func()
	CFRunLoopStop              func(uintptr)
	CFRunLoopRunInMode         func(mode uintptr, seconds float64, returnAfterSource bool) int32
	CFRunLoopAddSource         func(rl, src, mode uintptr)
	CFMachPortCreateRunLoopSrc func(alloc, port uintptr, order int) uintptr
	CFMachPortInvalidate       func(uintptr)

	CFURLCreateFromFileSystemRepresentation func(alloc uintptr, buf *byte, n int, dir bool) uintptr
	CFURLGetFileSystemRepresentation        func(url uintptr, resolve bool, buf *byte, n int) bool
	CFURLCopyResourcePropertyForKey         func(url, key uintptr, value, err *uintptr) bool
	CFURLCreateBookmarkDataFromFile         func(alloc, url uintptr, err *uintptr) uintptr
	CFURLCreateByResolvingBookmarkData      func(alloc, data uintptr, opts uint, rel, props uintptr, stale *bool, err *uintptr) uintptr

	CGWindowListCopyWindowInfo             func(option, relativeTo uint32) uintptr
	CGRectMakeWithDictionaryRepresentation func(dict uintptr, rect *Rect) bool
	CGEventTapCreate                       func(tap, place, options uint32, mask uint64, cb, refcon uintptr) uintptr
	CGEventTapEnable                       func(tap uintptr, enable bool)
	CGEventTapIsEnabled                    func(tap uintptr) bool
	CGEventGetLocation                     func(event uintptr) Point
	CGEventGetFlags                        func(event uintptr) uint64
	CGEventGetIntegerValueField            func(event uintptr, field uint32) int64
	CGEventCreate                          func(source uintptr) uintptr
	CGColorSpaceCreateWithName             func(name uintptr) uintptr
	CGColorSpaceRelease                    func(uintptr)
	CGDataProviderCreateWithData           func(info uintptr, data unsafe.Pointer, size int, release uintptr) uintptr
	CGDataProviderRelease                  func(uintptr)
	CGImageCreate                          func(w, h, bpc, bpp, bpr int, cs uintptr, info uint32, provider uintptr, decode uintptr, interpolate bool, intent int32) uintptr
	CGImageRelease                         func(uintptr)
	CGPreflightListenEventAccess           func() bool
	CGRequestListenEventAccess             func() bool

	AXIsProcessTrusted            func() bool
	AXIsProcessTrustedWithOptions func(options uintptr) bool

	procName func(pid int32, buf *byte, size uint32) int32
)

const utf8Encoding = 0x08000100

func loadCF() {
	cf := libs[coreFoundationPath]
	reg := func(fptr any, name string) { purego.RegisterLibFunc(fptr, cf, name) }
	reg(&CFRelease, "CFRelease")
	reg(&CFRetain, "CFRetain")
	reg(&CFArrayGetCount, "CFArrayGetCount")
	reg(&CFArrayGetValueAtIndex, "CFArrayGetValueAtIndex")
	reg(&CFDictionaryGetValue, "CFDictionaryGetValue")
	reg(&cfStringCreateWithCString, "CFStringCreateWithCString")
	reg(&CFStringGetCString, "CFStringGetCString")
	reg(&CFNumberGetValue, "CFNumberGetValue")
	reg(&CFDictionaryCreate, "CFDictionaryCreate")
	reg(&CFLocaleCopyPreferredLangs, "CFLocaleCopyPreferredLanguages")
	reg(&CFRunLoopGetCurrent, "CFRunLoopGetCurrent")
	reg(&CFRunLoopGetMain, "CFRunLoopGetMain")
	reg(&CFRunLoopRun, "CFRunLoopRun")
	reg(&CFRunLoopStop, "CFRunLoopStop")
	reg(&CFRunLoopRunInMode, "CFRunLoopRunInMode")
	reg(&CFRunLoopAddSource, "CFRunLoopAddSource")
	reg(&CFMachPortCreateRunLoopSrc, "CFMachPortCreateRunLoopSource")
	reg(&CFMachPortInvalidate, "CFMachPortInvalidate")
	reg(&CFURLCreateFromFileSystemRepresentation, "CFURLCreateFromFileSystemRepresentation")
	reg(&CFURLGetFileSystemRepresentation, "CFURLGetFileSystemRepresentation")
	reg(&CFURLCopyResourcePropertyForKey, "CFURLCopyResourcePropertyForKey")
	reg(&CFURLCreateBookmarkDataFromFile, "CFURLCreateBookmarkDataFromFile")
	reg(&CFURLCreateByResolvingBookmarkData, "CFURLCreateByResolvingBookmarkData")
	purego.RegisterLibFunc(&procName, libs[systemLib], "proc_name")
}

func loadCG() {
	cg := libs[coreGraphicsPath]
	reg := func(fptr any, name string) { purego.RegisterLibFunc(fptr, cg, name) }
	reg(&CGWindowListCopyWindowInfo, "CGWindowListCopyWindowInfo")
	reg(&CGRectMakeWithDictionaryRepresentation, "CGRectMakeWithDictionaryRepresentation")
	reg(&CGEventTapCreate, "CGEventTapCreate")
	reg(&CGEventTapEnable, "CGEventTapEnable")
	reg(&CGEventTapIsEnabled, "CGEventTapIsEnabled")
	reg(&CGEventGetLocation, "CGEventGetLocation")
	reg(&CGEventGetFlags, "CGEventGetFlags")
	reg(&CGEventGetIntegerValueField, "CGEventGetIntegerValueField")
	reg(&CGEventCreate, "CGEventCreate")
	reg(&CGColorSpaceCreateWithName, "CGColorSpaceCreateWithName")
	reg(&CGColorSpaceRelease, "CGColorSpaceRelease")
	reg(&CGDataProviderCreateWithData, "CGDataProviderCreateWithData")
	reg(&CGDataProviderRelease, "CGDataProviderRelease")
	reg(&CGImageCreate, "CGImageCreate")
	reg(&CGImageRelease, "CGImageRelease")
	reg(&CGPreflightListenEventAccess, "CGPreflightListenEventAccess")
	reg(&CGRequestListenEventAccess, "CGRequestListenEventAccess")
	purego.RegisterLibFunc(&cfDataCreate, libs[coreFoundationPath], "CFDataCreate")
	reg(&cgDataProviderCreateWithCFData, "CGDataProviderCreateWithCFData")
	ax := libs[appServicesPath]
	purego.RegisterLibFunc(&AXIsProcessTrusted, ax, "AXIsProcessTrusted")
	purego.RegisterLibFunc(&AXIsProcessTrustedWithOptions, ax, "AXIsProcessTrustedWithOptions")
}

// CFString makes a CFString (owned: CFRelease it). Toll-free bridged with NSString.
func CFString(s string) uintptr { return cfStringCreateWithCString(0, s+"\x00", utf8Encoding) }

// GoCFString reads a CFString.
func GoCFString(s uintptr) string {
	if s == 0 {
		return ""
	}
	return GoString(ID(s))
}

// CFInt reads a CFNumber as int32 (kCFNumberSInt32Type).
func CFInt(n uintptr) (int32, bool) {
	var v int32
	if n == 0 || !CFNumberGetValue(n, 3, unsafe.Pointer(&v)) {
		return 0, false
	}
	return v, true
}

// CFFloat reads a CFNumber as double (kCFNumberFloat64Type).
func CFFloat(n uintptr) (float64, bool) {
	var v float64
	if n == 0 || !CFNumberGetValue(n, 6, unsafe.Pointer(&v)) {
		return 0, false
	}
	return v, true
}

// ProcessName is the executable name of a process (as ps shows it), "" when unknown.
func ProcessName(pid int32) string {
	buf := make([]byte, 256)
	n := procName(pid, &buf[0], uint32(len(buf)))
	if n <= 0 {
		return ""
	}
	return string(buf[:n])
}

var (
	srgb      uintptr
	imageInfo = uint32(1 /* kCGImageAlphaPremultipliedLast */) | 0 /* kCGBitmapByteOrderDefault: R,G,B,A in memory */
)

// CGImage makes a CGImage from premultiplied RGBA pixels (owned: CGImageRelease
// it). The pixels are copied, so img may change afterwards.
func CGImage(img *image.RGBA) uintptr {
	if srgb == 0 {
		srgb = CGColorSpaceCreateWithName(uintptr(Constant("kCGColorSpaceSRGB")))
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if w == 0 || h == 0 {
		return 0
	}
	// A CFData copy owns the bytes; the provider keeps it.
	data := cfDataCreate(0, &img.Pix[0], len(img.Pix))
	provider := cgDataProviderCreateWithCFData(data)
	CFRelease(data)
	im := CGImageCreate(w, h, 8, 32, img.Stride, srgb, imageInfo, provider, 0, false, 0)
	CGDataProviderRelease(provider)
	return im
}

var (
	cfDataCreate                   func(alloc uintptr, bytes *byte, n int) uintptr
	cgDataProviderCreateWithCFData func(data uintptr) uintptr
)
