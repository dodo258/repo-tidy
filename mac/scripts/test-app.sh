#!/bin/sh
# 验收打包好的 App 自带命令行、下载即用：
#   1. 跑 build-app.sh，查 Contents/Helpers/repo-tidy 是 arm64 + x86_64 两个架构；
#   2. 用临时 HOME、清空 PATH、不设 REPO_TIDY_BIN 启动 App 的截图模式（不给 --cli，
#      和正常打开一样自己找命令行），确认用的是 App 自带的，并且扫得出演示仓库。
# 只碰临时目录。用法：mac/scripts/test-app.sh [debug|release]，默认 release。
set -eu
cd "$(dirname "$0")/.."
app=$(scripts/build-app.sh "${1:-release}" | tail -n 1)
helper="$app/Contents/Helpers/repo-tidy"

archs=$(lipo -archs "$helper")
echo "自带命令行的架构：$archs"
case " $archs " in *" arm64 "*) ;; *) echo "缺 arm64" >&2; exit 1 ;; esac
case " $archs " in *" x86_64 "*) ;; *) echo "缺 x86_64" >&2; exit 1 ;; esac

tmp=$(mktemp -d)
tmp=$(cd "$tmp" && pwd -P)
trap 'rm -rf "$tmp"' EXIT
demo=$(scripts/demo-repos.sh "$tmp/demo" | tail -n 1)
mkdir -p "$tmp/home"

# NSHomeDirectory() 不看 $HOME，要靠 CFFIXED_USER_HOME 才会换成临时 HOME，
# 否则 App 会找到本机 ~/.local/bin 里装的 repo-tidy，测不出自带的那份。

out=$(env -i HOME="$tmp/home" CFFIXED_USER_HOME="$tmp/home" PATH= TMPDIR="$tmp/" \
  GIT_CONFIG_GLOBAL="$demo/.gitconfig" GIT_CONFIG_NOSYSTEM=1 XDG_STATE_HOME="$tmp/state" \
  "$app/Contents/MacOS/RepoTidy" --screenshot "$tmp/shots" \
  --messy "$demo/projects" --clean "$demo/clean" --empty "$demo/empty" --home "$demo") || {
  echo "App 截图模式失败：$out" >&2
  exit 1
}
echo "$out" | grep -qxF "命令行：$helper" || { echo "用的不是 App 自带的命令行：$out" >&2; exit 1; }
for s in 1-empty 3-removable 3-outcome 4-all-clean 5-failed; do
  [ -s "$tmp/shots/$s.png" ] || { echo "缺截图 $s.png" >&2; exit 1; }
done
echo "App 自带命令行，清空 PATH 也能扫描演示仓库：通过"
