#!/bin/sh
# 把 SwiftUI 界面打包成 mac/build/RepoTidy.app（本机用，ad-hoc 签名，不上架）。
# 用法：mac/scripts/build-app.sh [debug|release]，默认 release。
set -eu
cd "$(dirname "$0")/.."
config=${1:-release}
swift build -c "$config" --product RepoTidyApp
bin=$(swift build -c "$config" --show-bin-path)/RepoTidyApp
app=build/RepoTidy.app
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$bin" "$app/Contents/MacOS/RepoTidy"
cp Resources/Info.plist "$app/Contents/Info.plist"
codesign --force --sign - "$app" >/dev/null
echo "$PWD/$app"
