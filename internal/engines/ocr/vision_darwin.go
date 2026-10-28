package ocr

// Text recognition with Apple's Vision framework (VNRecognizeTextRequest),
// part of macOS: nothing to download, it runs on the Neural Engine or GPU.
// Called through the Objective-C runtime with purego (no cgo); the framework
// is loaded the first time it is asked for. Objects made with alloc/init are
// released, the ones Vision hands back go with the autorelease pool of each call.

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const visionFramework = "/System/Library/Frameworks/Vision.framework/Vision"

type appleVision struct{}

func systemRecognizer() TextRecognizer { return appleVision{} }

func osVersion() string {
	v, _ := syscall.Sysctl("kern.osproductversion")
	return v
}

var visionLoaded = sync.OnceValue(func() bool {
	if _, err := purego.Dlopen(visionFramework, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return false
	}
	return objc.GetClass("VNRecognizeTextRequest") != 0
})

var visionLanguages = sync.OnceValue(readVisionLanguages)

// sendRange is objc_msgSend typed for a method taking an NSRange and an
// NSError** (boundingBoxForRange:error:): purego lays the struct out as the
// ABI wants only with a typed signature.
var sendRange = sync.OnceValue(func() func(objc.ID, objc.SEL, nsRange, *objc.ID) objc.ID {
	var fn func(objc.ID, objc.SEL, nsRange, *objc.ID) objc.ID
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return nil
	}
	addr, err := purego.Dlsym(lib, "objc_msgSend")
	if err != nil {
		return nil
	}
	purego.RegisterFunc(&fn, addr)
	return fn
})

func (appleVision) Available() bool { return visionLoaded() }

func (a appleVision) Languages() []string {
	if !a.Available() {
		return nil
	}
	return visionLanguages()
}

type cgRect struct{ X, Y, W, H float64 }

type nsRange struct{ Location, Length uint }

var selCache sync.Map

func sel(name string) objc.SEL {
	if s, ok := selCache.Load(name); ok {
		return s.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func newObject(name string) objc.ID { return class(name).Send(sel("alloc")).Send(sel("init")) }

func nsString(s string) objc.ID { return class("NSString").Send(sel("stringWithUTF8String:"), s) }

func goString(ns objc.ID) string {
	if ns == 0 {
		return ""
	}
	return objc.Send[string](ns, sel("UTF8String"))
}

// newRequest is a request at the accurate level and the newest revision this macOS has (owned: release it).
func newRequest() objc.ID {
	r := newObject("VNRecognizeTextRequest")
	r.Send(sel("setRecognitionLevel:"), 0) // accurate
	// Without it the revision depends on the SDK the program was built with.
	if revs := class("VNRecognizeTextRequest").Send(sel("supportedRevisions")); revs != 0 {
		if newest := objc.Send[uint](revs, sel("lastIndex")); newest != ^uint(0)>>1 && newest != ^uint(0) {
			r.Send(sel("setRevision:"), newest)
		}
	}
	return r
}

func readVisionLanguages() []string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := newObject("NSAutoreleasePool")
	defer pool.Send(sel("drain"))
	r := newRequest()
	defer r.Send(sel("release"))
	var nserr objc.ID
	list := r.Send(sel("supportedRecognitionLanguagesAndReturnError:"), &nserr)
	if list == 0 {
		return VisionFallbackLanguages
	}
	var out []string
	n := objc.Send[uint](list, sel("count"))
	for i := uint(0); i < n; i++ {
		if s := goString(list.Send(sel("objectAtIndex:"), i)); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return VisionFallbackLanguages
	}
	return out
}

func (a appleVision) Recognize(ctx context.Context, picture string, languages []string) ([]Line, error) {
	if !a.Available() {
		return nil, errors.New("Apple Vision text recognition is not available.")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := newObject("NSAutoreleasePool")
	defer pool.Send(sel("drain"))

	req := newRequest()
	defer req.Send(sel("release"))
	req.Send(sel("setUsesLanguageCorrection:"), true)
	wanted := class("NSMutableArray").Send(sel("array"))
	for _, l := range languages {
		wanted.Send(sel("addObject:"), nsString(l))
	}
	req.Send(sel("setRecognitionLanguages:"), wanted)

	url := class("NSURL").Send(sel("fileURLWithPath:"), nsString(picture))
	options := class("NSDictionary").Send(sel("dictionary"))
	handler := class("VNImageRequestHandler").Send(sel("alloc")).Send(sel("initWithURL:options:"), url, options)
	defer handler.Send(sel("release"))
	requests := class("NSArray").Send(sel("arrayWithObject:"), req)

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			req.Send(sel("cancel"))
		case <-stop:
		}
	}()
	var nserr objc.ID
	ok := objc.Send[bool](handler, sel("performRequests:error:"), requests, &nserr)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !ok {
		reason := ""
		if nserr != 0 {
			reason = goString(nserr.Send(sel("localizedDescription")))
		}
		return nil, errors.New(strings.TrimSpace("Apple Vision couldn't read the picture. " + reason))
	}
	return visionLines(req.Send(sel("results"))), nil
}

// visionLines are the recognized lines, each with a box per word (estimated from the line when Vision gives none).
func visionLines(observations objc.ID) []Line {
	var lines []Line
	if observations == 0 {
		return nil
	}
	n := objc.Send[uint](observations, sel("count"))
	for i := uint(0); i < n; i++ {
		obs := observations.Send(sel("objectAtIndex:"), i)
		cands := obs.Send(sel("topCandidates:"), uint(1))
		if cands == 0 || objc.Send[uint](cands, sel("count")) == 0 {
			continue
		}
		cand := cands.Send(sel("objectAtIndex:"), uint(0))
		text := goString(cand.Send(sel("string")))
		if strings.TrimSpace(text) == "" {
			continue
		}
		box := objc.Send[cgRect](obs, sel("boundingBox"))
		est := EstimateLine(text, box.X, box.Y, box.W, box.H)
		var words []Word
		for w, r := range WordRanges(text) {
			// NSString ranges count UTF-16 units, as WordRanges does.
			var nserr objc.ID
			var wb objc.ID
			if send := sendRange(); send != nil {
				wb = send(cand, sel("boundingBoxForRange:error:"), nsRange{uint(r[0]), uint(r[1])}, &nserr)
			}
			if wb == 0 {
				words = append(words, est.Words[w])
				continue
			}
			rect := objc.Send[cgRect](wb, sel("boundingBox"))
			words = append(words, Word{est.Words[w].Text, rect.X, rect.W})
		}
		lines = append(lines, Line{box.X, box.Y, box.W, box.H, words})
	}
	return lines
}
