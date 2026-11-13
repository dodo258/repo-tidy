#!/bin/sh
# 把 SwiftUI 界面打包成 mac/build/RepoTidy.app（本机用，ad-hoc 签名，不上架）。
# 同一份源码编的 repo-tidy 命令行（arm64 + x86_64，lipo 合成一个）放进 Contents/Helpers/，
# App 不用另装命令行就能用。要 Go（版本见 go.mod）和 Swift 6 工具链。
# 用法：mac/scripts/build-app.sh [debug|release]，默认 release。
set -eu
cd "$(dirname "$0")/.."
config=${1:-release}
swift build -c "$config" --product RepoTidyApp
bin=$(swift build -c "$config" --show-bin-path)/RepoTidyApp
app=build/RepoTidy.app
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources" "$app/Contents/Helpers"
cp "$bin" "$app/Contents/MacOS/RepoTidy"
cp Resources/Info.plist "$app/Contents/Info.plist"

# 命令行：两个架构各编一份再合并。纯 Go、不用 cgo，交叉编译不需要别的工具链。
cli=$(mktemp -d)
trap 'rm -rf "$cli"' EXIT
for arch in arm64 amd64; do
  (cd .. && CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath -o "$cli/repo-tidy-$arch" ./cmd/repo-tidy)
done
lipo -create -output "$app/Contents/Helpers/repo-tidy" "$cli/repo-tidy-arm64" "$cli/repo-tidy-amd64"

# 先签里面的命令行，再签整个 App
codesign --force --sign - "$app/Contents/Helpers/repo-tidy" >/dev/null
codesign --force --sign - "$app" >/dev/null
echo "$PWD/$app"
