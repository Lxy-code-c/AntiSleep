#!/bin/bash
# 构建 AntiSleep.app（通用二进制，arm64 + x86_64），并压成 zip 便于分发。
set -euo pipefail
cd "$(dirname "$0")"

APP="AntiSleep.app"
ZIP="AntiSleep-macOS.zip"
MIN="13.0"

command -v swiftc >/dev/null || { echo "需要安装 Xcode Command Line Tools：xcode-select --install"; exit 1; }

rm -rf build "$APP" "$ZIP"
mkdir -p build

echo "→ 编译 arm64"
swiftc -O -swift-version 5 -target arm64-apple-macosx${MIN} -parse-as-library App.swift -o build/AntiSleep-arm64

echo "→ 编译 x86_64"
if swiftc -O -swift-version 5 -target x86_64-apple-macosx${MIN} -parse-as-library App.swift -o build/AntiSleep-x86_64; then
  echo "→ 合并为通用二进制"
  lipo -create -output build/AntiSleep build/AntiSleep-arm64 build/AntiSleep-x86_64
else
  echo "⚠ x86_64 编译失败，仅输出 arm64 版本"
  cp build/AntiSleep-arm64 build/AntiSleep
fi
lipo -info build/AntiSleep

mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp Info.plist "$APP/Contents/Info.plist"
VER="${VERSION:-}"
if [[ -n "$VER" ]]; then
  /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $VER" "$APP/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c "Set :CFBundleVersion $VER" "$APP/Contents/Info.plist"
  echo "→ 版本号写入 $VER"
fi
cp build/AntiSleep "$APP/Contents/MacOS/AntiSleep"
chmod +x "$APP/Contents/MacOS/AntiSleep"

if [[ -f AppIcon.png ]]; then
  ICONSET=build/AppIcon.iconset
  rm -rf "$ICONSET"; mkdir -p "$ICONSET"
  for s in 16 32 128 256 512; do
    d=$((s*2))
    sips -z $s $s AppIcon.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
    sips -z $d $d AppIcon.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
  done
  iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"
  echo "→ 图标已生成"
fi

echo "→ ad-hoc 签名"
codesign --force --deep --sign - "$APP"

echo "→ 打包"
ditto -c -k --keepParent "$APP" "$ZIP"
echo "完成：$(pwd)/$ZIP"
