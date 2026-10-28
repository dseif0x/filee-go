#!/usr/bin/env bash
# Builds Filee.app: darwin/arm64 and darwin/amd64 cross-compiled (no cgo),
# merged into one universal executable by scripts/macapp (no lipo needed), so
# it runs on Linux too. On a Mac the app is then signed ad hoc; elsewhere sign
# it on a Mac before handing it out:
#
#   codesign --force --sign - --entitlements dist/entitlements.plist dist/Filee.app
#
# VERSION (default: git describe) and OUT (default: dist) can be set.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${VERSION:-$(git describe --tags --always 2>/dev/null || echo 0.1.0-dev)}
VERSION=${VERSION#v}
OUT=${OUT:-dist}
mkdir -p "$OUT"

export CGO_ENABLED=0
# nodynamic: the WebAssembly codecs (webp, avif, jpegxl) never look for a system library.
TAGS=nodynamic
LDFLAGS="-s -w -X github.com/dseif0x/filee-go/internal/app.Version=$VERSION"
for arch in arm64 amd64; do
	echo "building darwin/$arch"
	GOOS=darwin GOARCH=$arch go build -tags "$TAGS" -trimpath -ldflags "$LDFLAGS" -o "$OUT/filee-darwin-$arch" ./cmd/filee
done

go run ./scripts/macapp -version "$VERSION" \
	-arm64 "$OUT/filee-darwin-arm64" -amd64 "$OUT/filee-darwin-amd64" \
	-icon internal/gui/filee.png -out "$OUT/Filee.app"

if [ "$(uname -s)" = Darwin ] && command -v codesign >/dev/null; then
	# Ad hoc: Accessibility and Automation then know the app by this signature. A new build
	# signs differently, so after an update macOS may ask again (remove Filee from the list, add it back).
	codesign --force --sign - --entitlements "$OUT/entitlements.plist" --timestamp=none "$OUT/Filee.app"
	codesign --verify --verbose=2 "$OUT/Filee.app"
	ditto -c -k --keepParent "$OUT/Filee.app" "$OUT/Filee-$VERSION-macos-universal.zip"
else
	echo "not on a Mac: Filee.app is not signed as a bundle (the arm64 slice carries the linker's ad hoc signature)"
	go run ./scripts/macapp -version "$VERSION" \
		-arm64 "$OUT/filee-darwin-arm64" -amd64 "$OUT/filee-darwin-amd64" \
		-icon internal/gui/filee.png -out "$OUT/Filee.app" -zip "$OUT/Filee-$VERSION-macos-universal-unsigned.zip"
fi
rm -f "$OUT/filee-darwin-arm64" "$OUT/filee-darwin-amd64"
echo "done: $OUT/Filee.app"
