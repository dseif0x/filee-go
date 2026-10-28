//go:build darwin

// Package ns is the small part of the Objective-C runtime, Foundation,
// AppKit, Core Foundation and Core Graphics that Filee uses, called through
// purego (no cgo). Rules every caller keeps:
//
//   - AppKit objects are touched on the main thread only (OnMain / Sync);
//     the main goroutine is locked to it by internal/gui's init.
//   - Code that makes autoreleased objects off AppKit's own callbacks runs
//     inside WithPool (which also keeps the goroutine on one thread).
//   - Go functions are never passed as message arguments (purego would make
//     a new callback each time): callbacks are made once, at package level.
//   - BOOL results are read as their low byte (x86_64 returns a signed char).
package ns

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// ID is an Objective-C object (nil is 0).
type ID = objc.ID

// SEL is a selector.
type SEL = objc.SEL

// Point, Size and Rect are NSPoint/CGPoint, NSSize/CGSize and NSRect/CGRect (CGFloat is double on 64-bit).
type Point struct{ X, Y float64 }
type Size struct{ Width, Height float64 }
type Rect struct {
	Origin Point
	Size   Size
}

// Frameworks Filee loads.
const (
	objcLib            = "/usr/lib/libobjc.A.dylib"
	systemLib          = "/usr/lib/libSystem.B.dylib"
	foundationPath     = "/System/Library/Frameworks/Foundation.framework/Foundation"
	appKitPath         = "/System/Library/Frameworks/AppKit.framework/AppKit"
	coreFoundationPath = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	coreGraphicsPath   = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
	appServicesPath    = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
	quartzCorePath     = "/System/Library/Frameworks/QuartzCore.framework/QuartzCore"
	webKitPath         = "/System/Library/Frameworks/WebKit.framework/WebKit"
	notificationsPath  = "/System/Library/Frameworks/UserNotifications.framework/UserNotifications"
	serviceMgmtPath    = "/System/Library/Frameworks/ServiceManagement.framework/ServiceManagement"
)

var (
	loadOnce sync.Once
	loadErr  error

	// Libraries by path, for Dlsym.
	libs = map[string]uintptr{}

	objc_autoreleasePoolPush func() uintptr
	objc_autoreleasePoolPop  func(uintptr)
	dispatch_async_f         func(queue, ctx, fn uintptr)
	mainQueue                uintptr
	pthread_main_np          func() int32
)

// Load opens the frameworks (once). Optional ones (WebKit, UserNotifications,
// ServiceManagement) that fail to load are left out; their users check Has.
func Load() error {
	loadOnce.Do(func() {
		for _, p := range []string{objcLib, systemLib, foundationPath, coreFoundationPath, coreGraphicsPath, appServicesPath, quartzCorePath, appKitPath} {
			h, err := purego.Dlopen(p, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				loadErr = fmt.Errorf("loading %s: %w", p, err)
				return
			}
			libs[p] = h
		}
		for _, p := range []string{webKitPath, notificationsPath, serviceMgmtPath} {
			if h, err := purego.Dlopen(p, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
				libs[p] = h
			}
		}
		purego.RegisterLibFunc(&objc_autoreleasePoolPush, libs[objcLib], "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&objc_autoreleasePoolPop, libs[objcLib], "objc_autoreleasePoolPop")
		purego.RegisterLibFunc(&dispatch_async_f, libs[systemLib], "dispatch_async_f")
		purego.RegisterLibFunc(&pthread_main_np, libs[systemLib], "pthread_main_np")
		q, err := purego.Dlsym(libs[systemLib], "_dispatch_main_q")
		if err != nil {
			loadErr = fmt.Errorf("dispatch main queue: %w", err)
			return
		}
		mainQueue = q
		dispatchCallback = purego.NewCallback(runDispatched)
		loadCF()
		loadCG()
	})
	return loadErr
}

// Loaded reports whether an optional framework is there ("WebKit", "UserNotifications", "ServiceManagement").
func Loaded(name string) bool {
	switch name {
	case "WebKit":
		return libs[webKitPath] != 0
	case "UserNotifications":
		return libs[notificationsPath] != 0
	case "ServiceManagement":
		return libs[serviceMgmtPath] != 0
	}
	return false
}

func mustSym(lib, name string) uintptr {
	p, err := purego.Dlsym(libs[lib], name)
	if err != nil {
		panic(fmt.Sprintf("ns: %s: %v", name, err))
	}
	return p
}

// symbolValue reads a global pointer constant (an exported NSString * or CFStringRef).
func symbolValue(lib, name string) uintptr {
	p, err := purego.Dlsym(libs[lib], name)
	if err != nil || p == 0 {
		return 0
	}
	return **(**uintptr)(unsafe.Pointer(&p))
}

// Constant is an exported object constant of AppKit or Foundation by name
// (NSPasteboardTypeFileURL, kCFRunLoopCommonModes, …); 0 when missing.
func Constant(name string) ID {
	for _, lib := range []string{appKitPath, foundationPath, coreFoundationPath, coreGraphicsPath, appServicesPath, quartzCorePath, webKitPath, notificationsPath} {
		if libs[lib] == 0 {
			continue
		}
		if v := symbolValue(lib, name); v != 0 {
			return ID(v)
		}
	}
	return 0
}

var selectors sync.Map // string → SEL

// Sel is the selector of name (cached).
func Sel(name string) SEL {
	if s, ok := selectors.Load(name); ok {
		return s.(SEL)
	}
	s := objc.RegisterName(name)
	selectors.Store(name, s)
	return s
}

// Class is the class named name as an object (0 when it doesn't exist).
func Class(name string) ID { return ID(objc.GetClass(name)) }

// Send sends a message that returns an object or nothing.
func Send(id ID, sel string, args ...any) ID {
	if id == 0 {
		return 0
	}
	return id.Send(Sel(sel), args...)
}

// Bool sends a message that returns BOOL.
func Bool(id ID, sel string, args ...any) bool {
	if id == 0 {
		return false
	}
	return objc.Send[uint8](id, Sel(sel), args...) != 0
}

// Int sends a message that returns NSInteger.
func Int(id ID, sel string, args ...any) int {
	if id == 0 {
		return 0
	}
	return objc.Send[int](id, Sel(sel), args...)
}

// Uint sends a message that returns NSUInteger.
func Uint(id ID, sel string, args ...any) uint {
	if id == 0 {
		return 0
	}
	return objc.Send[uint](id, Sel(sel), args...)
}

// Float sends a message that returns CGFloat/double.
func Float(id ID, sel string, args ...any) float64 {
	if id == 0 {
		return 0
	}
	return objc.Send[float64](id, Sel(sel), args...)
}

// GetPoint, GetSize and GetRect send messages that return those structs
// (objc.Send uses objc_msgSend_stret for NSRect on x86_64).
func GetPoint(id ID, sel string, args ...any) Point {
	if id == 0 {
		return Point{}
	}
	return objc.Send[Point](id, Sel(sel), args...)
}

func GetSize(id ID, sel string, args ...any) Size {
	if id == 0 {
		return Size{}
	}
	return objc.Send[Size](id, Sel(sel), args...)
}

func GetRect(id ID, sel string, args ...any) Rect {
	if id == 0 {
		return Rect{}
	}
	return objc.Send[Rect](id, Sel(sel), args...)
}

// New is [[cls alloc] init] (owned: release it, or keep it for good).
func New(class string) ID { return Send(Send(Class(class), "alloc"), "init") }

// Alloc is [cls alloc].
func Alloc(class string) ID { return Send(Class(class), "alloc") }

// Retain and Release adjust the reference count.
func Retain(id ID) ID { return Send(id, "retain") }
func Release(id ID) {
	if id != 0 {
		Send(id, "release")
	}
}

// String makes an autoreleased NSString.
func String(s string) ID {
	return Send(Class("NSString"), "stringWithUTF8String:", s+"\x00")
}

// GoString reads an NSString (or "" for nil / anything else).
func GoString(s ID) string {
	if s == 0 || !Bool(s, "isKindOfClass:", Class("NSString")) {
		return ""
	}
	p := objc.Send[*byte](s, Sel("UTF8String"))
	return cstring(p)
}

func cstring(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

// Strings reads the NSStrings of an NSArray (other objects skipped).
func Strings(arr ID) []string {
	var out []string
	for _, o := range Array(arr) {
		if Bool(o, "isKindOfClass:", Class("NSString")) {
			out = append(out, GoString(o))
		}
	}
	return out
}

// Array is the objects of an NSArray (none for nil or anything else).
func Array(arr ID) []ID {
	if arr == 0 || !Bool(arr, "isKindOfClass:", Class("NSArray")) {
		return nil
	}
	n := Uint(arr, "count")
	out := make([]ID, n)
	for i := range n {
		out[i] = Send(arr, "objectAtIndex:", i)
	}
	return out
}

// NSArray makes an autoreleased NSArray of the objects.
func NSArray(objs ...ID) ID {
	arr := Send(Class("NSMutableArray"), "arrayWithCapacity:", uint(len(objs)))
	for _, o := range objs {
		Send(arr, "addObject:", o)
	}
	return arr
}

// StringArray makes an autoreleased NSArray of NSStrings.
func StringArray(ss []string) ID {
	objs := make([]ID, len(ss))
	for i, s := range ss {
		objs[i] = String(s)
	}
	return NSArray(objs...)
}

// FileURL is an autoreleased NSURL for a path.
func FileURL(path string) ID { return Send(Class("NSURL"), "fileURLWithPath:", String(path)) }

// URL is an autoreleased NSURL from a string (nil when malformed).
func URL(s string) ID { return Send(Class("NSURL"), "URLWithString:", String(s)) }

// ErrorText is an NSError's localized description.
func ErrorText(err ID) string { return GoString(Send(err, "localizedDescription")) }

// WithPool runs f inside an autorelease pool on one OS thread.
func WithPool(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc_autoreleasePoolPush()
	defer objc_autoreleasePoolPop(pool)
	f()
}

// IsMainThread reports whether the caller runs on the main thread.
func IsMainThread() bool { return pthread_main_np() != 0 }

var (
	dispatchCallback uintptr
	dispatchMu       sync.Mutex
	dispatchNext     uintptr
	dispatched       = map[uintptr]func(){}
)

func runDispatched(ctx uintptr) {
	dispatchMu.Lock()
	f := dispatched[ctx]
	delete(dispatched, ctx)
	dispatchMu.Unlock()
	if f == nil {
		return
	}
	pool := objc_autoreleasePoolPush()
	defer objc_autoreleasePoolPop(pool)
	defer func() {
		if r := recover(); r != nil {
			if PanicHandler != nil {
				PanicHandler(r)
			}
		}
	}()
	f()
}

// PanicHandler gets a panic of a function run on the main thread (it must
// not unwind into AppKit).
var PanicHandler func(any)

// OnMain runs f on the main thread, asynchronously (from the main thread
// too: after the current event).
func OnMain(f func()) {
	dispatchMu.Lock()
	dispatchNext++
	id := dispatchNext
	dispatched[id] = f
	dispatchMu.Unlock()
	dispatch_async_f(mainQueue, id, dispatchCallback)
}

// Sync runs f on the main thread and waits for it (at once when already there).
func Sync(f func()) {
	if IsMainThread() {
		f()
		return
	}
	done := make(chan struct{})
	OnMain(func() {
		defer close(done)
		f()
	})
	<-done
}

// ErrNotLoaded: Load wasn't called or failed.
var ErrNotLoaded = errors.New("ns: frameworks not loaded")

// SymbolAddress is the address of an exported symbol (a struct constant such
// as kCFTypeDictionaryKeyCallBacks), 0 when missing.
func SymbolAddress(name string) uintptr {
	for _, lib := range []string{coreFoundationPath, foundationPath, appKitPath, coreGraphicsPath, appServicesPath} {
		if p, err := purego.Dlsym(libs[lib], name); err == nil && p != 0 {
			return p
		}
	}
	return 0
}
