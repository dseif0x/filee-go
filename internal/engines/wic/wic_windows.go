//go:build windows

package wic

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/dseif0x/filee-go/internal/engines/images"
)

// Windows Imaging Component through COM, the part Filee needs: a decoder for
// a file, its first frame, the frame's pixel format and a format converter to
// read the pixels in a known layout. Integer formats are read as 8-bit BGRA;
// fixed-point and float ones are linear scRGB (HDR screenshots are half
// floats), read as 128-bit floats and tone-mapped.

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

func guid(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic(err)
	}
	return g
}

var (
	clsidImagingFactory  = guid("{cacaf262-9370-4615-a13b-9f5539da4c0a}")
	iidImagingFactory    = guid("{ec5ec8a9-c395-4314-9c77-54d7a935ff70}")
	clsidJpegXrDecoder   = guid("{a26cec36-234c-4950-ae16-e34aace71d0d}")
	iidUnknown           = guid("{00000000-0000-0000-C000-000000000046}")
	iidPixelFormatInfo2  = guid("{a9db33a2-af5f-43c7-b679-74f5984b5aa4}")
	format32bppBGRA      = guid("{6fddc324-4e03-4bfe-b185-3d77768dc90f}")
	format128bppRGBAFlt  = guid("{6fddc324-4e03-4bfe-b185-3d77768dc919}")
	errComponentNotFound = uint32(0x88982F50)
)

const (
	clsctxInprocServer = 1
	genericRead        = 0x80000000
	numericFixed       = 4
	numericFloat       = 5
	bandBytes          = 4 << 20
)

// vtable indexes
const (
	vRelease                  = 2
	vQueryInterface           = 0
	vCreateDecoderFromFile    = 3
	vCreateComponentInfo      = 6
	vCreateFormatConverter    = 10
	vGetFrameCount            = 12
	vGetFrame                 = 13
	vGetSize                  = 3
	vGetPixelFormat           = 4
	vGetResolution            = 5
	vCopyPixels               = 7
	vConverterInitialize      = 8
	vSupportsTransparency     = 16
	vGetNumericRepresentation = 17
)

type hresultError struct {
	what string
	hr   uint32
}

func (e *hresultError) Error() string { return fmt.Sprintf("%s failed (0x%08X)", e.what, e.hr) }

// call calls method index of a COM object.
func call(obj unsafe.Pointer, index int, args ...uintptr) uint32 {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return uint32(r)
}

func failed(hr uint32) bool { return int32(hr) < 0 }

func release(obj unsafe.Pointer) {
	if obj != nil {
		call(obj, vRelease)
	}
}

// comThread locks the goroutine to its thread and initializes COM there.
func comThread() func() {
	runtime.LockOSThread()
	hr, _, _ := procCoInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
	return func() {
		if !failed(uint32(hr)) {
			procCoUninitialize.Call()
		}
		runtime.UnlockOSThread()
	}
}

func createInstance(clsid, iid *windows.GUID) (unsafe.Pointer, error) {
	var obj unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&obj)))
	if failed(uint32(hr)) {
		return nil, &hresultError{"CoCreateInstance", uint32(hr)}
	}
	return obj, nil
}

// codecAvailable: every Windows 10 and 11 has the JPEG XR codec; a stripped-down installation might not.
func codecAvailable() error {
	if err := ole32.Load(); err != nil {
		return err
	}
	done := comThread()
	defer done()
	obj, err := createInstance(&clsidJpegXrDecoder, &iidUnknown)
	if err != nil {
		return err
	}
	release(obj)
	return nil
}

func windowsVersion() string {
	major, minor, build := windows.RtlGetNtVersionNumbers()
	return fmt.Sprintf("Windows %d.%d.%d", major, minor, build&0xFFFF)
}

type wicRect struct{ X, Y, W, H int32 }

// read decodes the first frame of a JPEG XR file.
func read(path string) (*images.Frame, error) {
	done := comThread()
	defer done()
	factory, err := createInstance(&clsidImagingFactory, &iidImagingFactory)
	if err != nil {
		return nil, errors.New("the Windows Imaging Component is not available: " + err.Error())
	}
	defer release(factory)
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var decoder unsafe.Pointer
	if hr := call(factory, vCreateDecoderFromFile, uintptr(unsafe.Pointer(name)), 0, genericRead, 0, uintptr(unsafe.Pointer(&decoder))); failed(hr) {
		if hr == errComponentNotFound {
			return nil, errors.New("Windows can't read this file: it is not a JPEG XR picture, or Windows' JPEG XR codec is missing")
		}
		return nil, fmt.Errorf("Windows couldn't decode this JPEG XR picture (0x%08X)", hr)
	}
	// the decoder keeps the file open until it is released
	defer release(decoder)
	var count uint32
	if hr := call(decoder, vGetFrameCount, uintptr(unsafe.Pointer(&count))); failed(hr) || count == 0 {
		return nil, errors.New("the file holds no picture")
	}
	var frame unsafe.Pointer
	if hr := call(decoder, vGetFrame, 0, uintptr(unsafe.Pointer(&frame))); failed(hr) {
		return nil, &hresultError{"GetFrame", hr}
	}
	defer release(frame)
	var w, h uint32
	call(frame, vGetSize, uintptr(unsafe.Pointer(&w)), uintptr(unsafe.Pointer(&h)))
	if w == 0 || h == 0 {
		return nil, errors.New("the picture is empty")
	}
	if uint64(w)*uint64(h)*4 > 1<<31 {
		return nil, fmt.Errorf("a %d × %d picture is too large to convert", w, h)
	}
	var format windows.GUID
	call(frame, vGetPixelFormat, uintptr(unsafe.Pointer(&format)))
	var info, pf unsafe.Pointer
	hdr, transparency := false, true
	if hr := call(factory, vCreateComponentInfo, uintptr(unsafe.Pointer(&format)), uintptr(unsafe.Pointer(&info))); !failed(hr) {
		defer release(info)
		if hr := call(info, vQueryInterface, uintptr(unsafe.Pointer(&iidPixelFormatInfo2)), uintptr(unsafe.Pointer(&pf))); !failed(hr) {
			defer release(pf)
			var numeric int32
			var supports int32
			call(pf, vGetNumericRepresentation, uintptr(unsafe.Pointer(&numeric)))
			call(pf, vSupportsTransparency, uintptr(unsafe.Pointer(&supports)))
			hdr = numeric == numericFloat || numeric == numericFixed
			transparency = supports != 0
		}
	}
	var conv unsafe.Pointer
	if hr := call(factory, vCreateFormatConverter, uintptr(unsafe.Pointer(&conv))); failed(hr) {
		return nil, &hresultError{"CreateFormatConverter", hr}
	}
	defer release(conv)
	target := &format32bppBGRA
	if hdr {
		target = &format128bppRGBAFlt
	}
	// Initialize(source, format, dither none, no palette, alpha threshold 0.0, custom palette)
	if hr := call(conv, vConverterInitialize, uintptr(frame), uintptr(unsafe.Pointer(target)), 0, 0, 0, 0); failed(hr) {
		return nil, &hresultError{"the pixel format conversion", hr}
	}
	width, height := int(w), int(h)
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	if hdr {
		if err := toneMapped(conv, img); err != nil {
			return nil, err
		}
	} else {
		rect := wicRect{0, 0, int32(width), int32(height)}
		if hr := call(conv, vCopyPixels, uintptr(unsafe.Pointer(&rect)), uintptr(width*4), uintptr(len(img.Pix)), uintptr(unsafe.Pointer(&img.Pix[0]))); failed(hr) {
			return nil, &hresultError{"CopyPixels", hr}
		}
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
		}
	}
	f := &images.Frame{Img: img, Alpha: transparency && !opaque(img)}
	var dx, dy float64
	call(frame, vGetResolution, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy)))
	if dx > 0 && dx < 100_000 && dy > 0 && dy < 100_000 {
		f.Meta.DpiX, f.Meta.DpiY = dx, dy
	}
	return f, nil
}

// toneMapped reads the picture in bands of floats, finds the reference white
// and the peak, then maps it to 8-bit sRGB.
func toneMapped(conv unsafe.Pointer, img *image.NRGBA) error {
	width, height := img.Rect.Dx(), img.Rect.Dy()
	stride := width * 4
	bandRows := max(1, min(height, bandBytes/(stride*4)))
	band := make([]float32, bandRows*stride)
	linear := make([]float32, stride*height)
	step := sampleStep(int64(width) * int64(height))
	var lum []float32
	peak := 0.0
	pixel := int64(0)
	for y := 0; y < height; y += bandRows {
		rows := min(bandRows, height-y)
		rect := wicRect{0, int32(y), int32(width), int32(rows)}
		if hr := call(conv, vCopyPixels, uintptr(unsafe.Pointer(&rect)), uintptr(stride*4), uintptr(rows*stride*4), uintptr(unsafe.Pointer(&band[0]))); failed(hr) {
			return &hresultError{"CopyPixels", hr}
		}
		copy(linear[y*stride:], band[:rows*stride])
		for i := 0; i < rows*stride; i += 4 {
			r, g, b := band[i], band[i+1], band[i+2]
			peak = max(peak, maxChannel(r, g, b))
			if pixel%step == 0 {
				lum = append(lum, luminance(r, g, b))
			}
			pixel++
		}
	}
	tm := newToneMap(referenceWhite(lum), peak)
	for i := 0; i < len(linear); i += 4 {
		tm.mapPixel(linear[i], linear[i+1], linear[i+2], linear[i+3], img.Pix[i:i+4])
	}
	return nil
}

func opaque(img *image.NRGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 255 {
			return false
		}
	}
	return true
}
