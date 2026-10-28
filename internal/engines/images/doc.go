// Package images is the raster image engine (id "magick", the C# original's
// ImageMagick engine, so saved engine priorities still match), built from
// pure-Go codecs: the standard library (PNG, JPEG, GIF), golang.org/x/image
// (BMP, TIFF), github.com/gen2brain/{webp,avif,heic,jpegxl} (libwebp, libavif,
// a HEIC decoder and libjxl as WebAssembly), github.com/mrjoshuak/go-jpeg2000,
// github.com/mrjoshuak/go-openexr, github.com/woozymasta/bcn (DDS), dcraw as
// WebAssembly for camera RAW, and Filee's own readers and writers for the rest
// (ICO/CUR, PSD/PSB, TGA, Netpbm, QOI, PCX, XPM, XBM, WBMP, SGI, Sun Raster,
// MIFF, MNG, APNG, XCF, Photo CD, Radiance HDR, DICOM, FITS).
//
// Builds for release should pass -tags nodynamic: the gen2brain packages
// otherwise use a system libwebp/libavif/libjxl through purego when one is
// installed, instead of their WebAssembly.
//
// # Reading and writing pictures from other engines
//
// Engines that take or produce raster pictures (PDF pages, vector rendering,
// OCR, icons) use this package rather than codecs of their own:
//
//   - Decode(path, format, preset) reads every frame of a file as a *Picture
//     (8-bit NRGBA frames with metadata; format "" detects by extension).
//     DecodeBytes does the same for data in memory.
//   - DecodeImage(path, format) is the first frame, upright, as *image.NRGBA.
//   - ApplyOptions(frame, &preset.Image, target) applies a preset's picture
//     options (turn, resize, hide areas, background, grey, metadata, …).
//   - Write(frames, target, step.Output, options, sourcePath, animation, loops)
//     writes frames through an output allocator as the image engine does
//     (one file per page, or one animation / multi-page file), and
//     WriteDecoded does decode-to-files for a picture another engine decoded.
//   - EncodeImage(img, format, options) / Encode(path, img, format, options)
//     encode one image with the format's encoder settings only.
//   - Readable, Writable, CanRead, CanWrite and Edge give the formats and the
//     edges an image engine offers.
package images
