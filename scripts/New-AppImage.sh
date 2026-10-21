#!/usr/bin/env bash
# Run on the target Linux architecture after building bin/PSNWDL.
# Deliberately uses system GTK4/WebKitGTK; do not add dependency bundlers here.
set -euo pipefail
cd "$(dirname "$0")/.."

case "$(uname -m)" in
  x86_64)
    arch=x86_64
    tool_sha=ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0
    runtime_sha=2fca8b443c92510f1483a883f60061ad09b46b978b2631c807cd873a47ec260d
    ;;
  aarch64)
    arch=aarch64
    tool_sha=f0837e7448a0c1e4e650a93bb3e85802546e60654ef287576f46c71c126a9158
    runtime_sha=00cbdfcf917cc6c0ff6d3347d59e0ca1f7f45a6df1a428a0d6d8a78664d87444
    ;;
  *) echo 'AppImage packaging requires native x64 or ARM64 Linux.' >&2; exit 1 ;;
esac

version=$(tr -d '[:space:]' < VERSION)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
curl --fail --location --retry 3 --output "$work/appimagetool" \
  "https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-$arch.AppImage"
curl --fail --location --retry 3 --output "$work/runtime" \
  "https://github.com/AppImage/type2-runtime/releases/download/20251108/runtime-$arch"
printf '%s  %s\n' "$tool_sha" "$work/appimagetool" "$runtime_sha" "$work/runtime" | sha256sum --check
chmod +x "$work/appimagetool"

appdir="$work/PSNWDL.AppDir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/licenses/psnwdl" staging
install -m755 bin/PSNWDL "$appdir/usr/bin/PSNWDL"
install -m644 build/linux/PSNWDL.desktop "$appdir/PSNWDL.desktop"
install -m644 build/appicon.png "$appdir/PSNWDL.png"
install -m644 LICENSE "$appdir/usr/share/licenses/psnwdl/LICENSE"
install -m755 build/linux/AppRun "$appdir/AppRun"
desktop-file-validate "$appdir/PSNWDL.desktop"
ARCH="$arch" APPIMAGE_EXTRACT_AND_RUN=1 "$work/appimagetool" \
  --no-appstream --runtime-file "$work/runtime" "$appdir" \
  "staging/psnwdl-$version-linux-$arch.AppImage"
