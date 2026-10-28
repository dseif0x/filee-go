//go:build darwin

package gui

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/dseif0x/filee-go/internal/macos/bundle"
	"github.com/dseif0x/filee-go/internal/macos/ns"
	"github.com/dseif0x/filee-go/internal/macos/pbpaths"
)

// "Convert with Filee" in Finder's right-click menu: a service Filee.app
// provides itself (NSServices in Info.plist). macOS hands the selected files
// straight to the running Filee. This registers the object that answers the
// message, an NSObject subclass made at run time with one method,
// convertFiles:userData:error:.

var (
	servicesProvider ns.ID
	servicesHandler  func([]string)
	servicesNothing  func([]string)
	servicesIMP      objc.IMP
)

func registerServices(handler, nothingUsable func([]string)) {
	servicesHandler, servicesNothing = handler, nothingUsable
	if servicesProvider != 0 {
		return
	}
	cls, err := objc.RegisterClass("FileeServicesProvider", objc.GetClass("NSObject"), nil, nil, nil)
	if err != nil {
		return
	}
	// The exact types AppKit builds its invocation from: void, self, _cmd, NSPasteboard*, NSString*, NSString**.
	servicesIMP = objc.NewIMP(func(_ objc.ID, _ objc.SEL, pb objc.ID, _ objc.ID, _ uintptr) {
		defer func() { _ = recover() }() // nothing may unwind into AppKit
		files := servicesFiles(pb)
		if len(files) > 0 {
			servicesHandler(files)
		} else if servicesNothing != nil {
			servicesNothing(ns.Strings(ns.Send(pb, "types")))
		}
	})
	cls.AddMethod(ns.Sel(bundle.ServiceMessage+":userData:error:"), servicesIMP, "v@:@@^@")
	servicesProvider = ns.Send(ns.ID(cls), "new")
	app := ns.Send(ns.Class("NSApplication"), "sharedApplication")
	ns.Send(app, "setServicesProvider:", servicesProvider)
	if p, err := purego.Dlsym(purego.RTLD_DEFAULT, "NSUpdateDynamicServices"); err == nil && p != 0 {
		purego.SyscallN(p)
	}
}

// servicesFiles are the existing files on the service's pasteboard: Finder's
// file URLs, another file manager's path list, or paths as text.
func servicesFiles(pb ns.ID) []string {
	return pbpaths.FirstExisting([]func() []string{
		func() []string { return fileURLs(pb) },
		func() []string {
			return ns.Strings(ns.Send(pb, "propertyListForType:", ns.String(pbpaths.FilenamesType)))
		},
		func() []string {
			var out []string
			for _, s := range itemStrings(pb, pbpaths.FileURLType) {
				if p := pbpaths.FromFileURL(s); p != "" {
					out = append(out, p)
				}
			}
			return out
		},
		func() []string {
			var out []string
			for _, s := range itemStrings(pb, pbpaths.TextType) {
				out = append(out, pbpaths.FromText(s)...)
			}
			return out
		},
	}, nil)
}
