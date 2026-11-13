#!/bin/sh
# 在给定目录（必须是临时目录）里造演示仓库，给截图和手动演示用：
#   <目录>/projects/  两个有可删项和要留项的仓库
#   <目录>/clean/  一个干净的仓库（只有一个锁定的 worktree）
#   <目录>/empty/  空文件夹
# 只用临时的 git 配置，不触发用户的全局钩子。DEMO_LANG=en 时提交标题和锁定原因用英文（英文截图、英文 README 用）。
set -eu
base=${1:?用法：demo-repos.sh <临时目录>}
case "$base" in
  "$HOME/projects"*) echo "不在 ~/projects 里造仓库" >&2; exit 2 ;;
esac
mkdir -p "$base"
base=$(cd "$base" && pwd -P)
export GIT_CONFIG_GLOBAL="$base/.gitconfig" GIT_CONFIG_NOSYSTEM=1
printf '[user]\n\tname = Demo\n\temail = demo@example.com\n[init]\n\tdefaultBranch = main\n[advice]\n\tdetachedHead = false\n' > "$GIT_CONFIG_GLOBAL"

ago() { date -v-"$1" '+%Y-%m-%dT%H:%M:%S'; }
# say <中文> <英文>：按 DEMO_LANG 选一句
say() { if [ "${DEMO_LANG:-zh}" = en ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }
# idle <worktree> <多久前>：把 worktree 的 index、HEAD、reflog 改成那时动的，
# 不然刚造出来的 worktree 会按「刚动过，可能还在用」留着。
idle() {
  gd=$(git -C "$1" rev-parse --absolute-git-dir)
  for f in "$gd/index" "$gd/HEAD" "$gd/logs/HEAD"; do
    [ -e "$f" ] && touch -t "$(date -v-"$2" '+%Y%m%d%H%M.%S')" "$f"
  done
  return 0
}
commit() { # commit <目录> <多久前> <文件> <内容> <说明>
  printf '%s\n' "$4" > "$1/$3"
  git -C "$1" add -A
  GIT_AUTHOR_DATE=$(ago "$2") GIT_COMMITTER_DATE=$(ago "$2") git -C "$1" commit -q -m "$5"
}

mkdir -p "$base/projects" "$base/clean" "$base/empty"

# ---- todo-app：worktree 和分支各种状态都有 ----
r="$base/projects/todo-app"
git init -q "$r"
commit "$r" 30d README.md "# todo-app" "$(say 初始化 "Initial commit")"
commit "$r" 20d .gitignore ".env
node_modules/
.claude/worktrees/" "$(say "忽略 .env、依赖和代理的 worktree" "Ignore .env, dependencies and agent worktrees")"
git init -q --bare "$base/projects/.backup-todo-app.git"
git -C "$r" remote add backup "$base/projects/.backup-todo-app.git"
# squash 合并过的分支，worktree 还在
git -C "$r" worktree add -q "$r/.claude/worktrees/1105-export-csv" -b agent/1105-export-csv
commit "$r/.claude/worktrees/1105-export-csv" 8d export.ts "export function toCSV() {}" "$(say "导出 CSV" "Export CSV")"
commit "$r/.claude/worktrees/1105-export-csv" 8d export.test.ts "test('csv')" "$(say "导出 CSV 的测试" "Tests for CSV export")"
git -C "$r" merge -q --squash agent/1105-export-csv >/dev/null
GIT_COMMITTER_DATE=$(ago 7d) git -C "$r" commit -q --date "$(ago 7d)" -m "$(say "合并 导出 CSV" "Merge: export CSV")"
# rebase 进 main 的分支
git -C "$r" switch -q -c agent/1107-fix-timezone
commit "$r" 6d date.ts "toLocal()" "$(say "日期按本地时区显示" "Show dates in local time zone")"
git -C "$r" switch -q main
GIT_COMMITTER_DATE=$(ago 5d) git -C "$r" cherry-pick agent/1107-fix-timezone >/dev/null
# 推到 backup、本地没有独有提交的分支
git -C "$r" switch -q -c agent/1109-search
commit "$r" 3d search.ts "search()" "$(say "搜索：先做标题" "Search: titles first")"
git -C "$r" push -q backup agent/1109-search main
git -C "$r" switch -q main
# 还在做、没合并也没推的 worktree（有未提交改动）
git -C "$r" worktree add -q "$r/.claude/worktrees/1111-dark-mode" -b agent/1111-dark-mode
commit "$r/.claude/worktrees/1111-dark-mode" 1d theme.ts "dark" "$(say "深色模式：颜色表" "Dark mode: color table")"
printf 'draft\n' > "$r/.claude/worktrees/1111-dark-mode/theme.draft.ts"
# 有 .env 的 worktree：内容已合并，但被忽略的 .env 主工作区没有
git -C "$r" worktree add -q "$r/.claude/worktrees/1102-api-keys" main~0 -b agent/1102-api-keys
printf 'API_KEY=demo\n' > "$r/.claude/worktrees/1102-api-keys/.env"
# 没合并的本地分支
git -C "$r" switch -q -c wip/voice-input
commit "$r" 12d voice.ts "listen()" "$(say "语音输入：试一下" "Voice input: first try")"
git -C "$r" switch -q main

idle "$r/.claude/worktrees/1105-export-csv" 7d
idle "$r/.claude/worktrees/1102-api-keys" 10d
idle "$r/.claude/worktrees/1111-dark-mode" 12M # 还在做：12 分钟前刚动过

# ---- site：一个可删，一个要留 ----
r="$base/projects/site"
git init -q "$r"
commit "$r" 40d index.html "<h1>site</h1>" "$(say 初始化 "Initial commit")"
git -C "$r" switch -q -c agent/1030-footer
commit "$r" 13d footer.html "<footer></footer>" "$(say 页脚 Footer)"
git -C "$r" switch -q main
git -C "$r" merge -q --no-ff -m "$(say "合并 页脚" "Merge: footer")" agent/1030-footer
git -C "$r" switch -q -c agent/1110-pricing
commit "$r" 2d pricing.html "<table></table>" "$(say "价格页：表格" "Pricing page: table")"
git -C "$r" switch -q main

# ---- notes：干净，只有一个锁定的 worktree ----
r="$base/clean/notes"
git init -q "$r"
commit "$r" 15d notes.md "# notes" "$(say 初始化 "Initial commit")"
git -C "$r" worktree add -q "$base/clean/notes-review" -b review
git -C "$r" worktree lock --reason "$(say 在用 "in use")" "$base/clean/notes-review"
idle "$base/clean/notes-review" 15d
# ---- dotfiles：干净，除了 main 什么都没有 ----
r="$base/clean/dotfiles"
git init -q "$r"
commit "$r" 60d zshrc "export EDITOR=vim" "$(say 初始化 "Initial commit")"
echo "$base"
