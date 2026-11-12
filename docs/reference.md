# 参考：判定规则、体检、JSON 字段、删除与恢复

用法和安装见 [README](../README.md)。

## 报告的顺序

开头是总览：扫了几个仓库（其中几个干净、几个扫描失败）、共多少附加 worktree 和本地分支、可删多少项、留着多少项（其中几项有未合并内容）、可删项能腾出多少空间。腾出的空间只算可删 worktree 的目录大小；删分支只删引用，提交对象要等 gc、还可能被别的分支用着，不算。

下面的仓库按乱的程度从多到少排：乱的程度 = 可删项数 + 留着的未合并项数（有既不在默认分支、也没推到远程的提交，或 worktree 里有未提交改动、未跟踪文件）。扫描失败的排在后面；干净的仓库（没有可删项、没有未合并内容）放在最后，每个只占一行，体检里有要注意的（未提交改动、stash、backup 没推到最新等）附在行尾。

## 判定

默认分支：`origin/HEAD` 指向的分支（本地也有时），否则 `main`、`master`；都没有就只列不判。

分支内容算「已在默认分支」，按顺序命中一条即可：

1. 分支末端是默认分支的祖先（证据：第一父链上第一个包含它的提交）；
2. 分支整段改动的 patch-id 等于默认分支上某个提交（squash）；
3. 分支每个提交在默认分支上都有同 patch-id 的提交（rebase）；
4. `git merge-tree` 把分支合进默认分支，结果和默认分支一模一样（证据：二分找到从哪个提交起已包含）。

都不满足时，看有没有远程跟踪引用包含分支末端，有就算「已推到远程」。

worktree 只在干净（没有改动、没有未跟踪文件、没有做到一半的操作、被忽略的文件都能重新生成或主工作区有同样一份）且检出的提交满足上面任一条时才算可删。目录已不在的 worktree 只清登记。

## 体检

每个仓库另有一段体检，只报不动：

- **占用**：`.git`、主工作区（不含 `.git` 和放在里面的附加 worktree）、附加 worktree 合计；每个 worktree 的大小附在它那一行末尾。按文件大小累加，不跟随符号链接。
- **主工作区**：当前分支相对上游领先/落后几个提交（没设上游、上游已不在、分离头指针都会写出来），未提交改动数（含未跟踪），stash 数。
- **最后提交**：本地分支里最新的一次提交时间和分支名。
- **backup**：逐个本地分支和 `backup` 上的同名分支比对，列出没推（backup 上没有）、落后（本地多出提交）、分叉（两边各有对方没有的提交）的分支。默认用本地的 `refs/remotes/backup/*`，即上次推送或 fetch 时的状态；加 `--online` 用 `git ls-remote backup` 查实际状态（超时 20 秒），联不上就写「联不上」和原因，再按本地记录比对。没有 `backup` 远程的标出来。

## JSON 输出

`--json` 把和文字报告同样的判定、体检结果写到标准输出（一个 JSON 文档，不夹别的文字），只读。和 `--apply` 一起用时输出的是删除结果，见「只删勾选的项」。退出码和文字报告一样：有仓库扫描失败是 1，参数错是 2；路径下一个仓库都没有时退出码 1、标准输出为空。字段只增不改，改了不兼容的地方 `version` 才加一。时间都是 RFC 3339，取不到时为 `null`。

```json
{
  "version": 1,
  "generated_at": "2026-11-12T17:47:33+08:00",
  "summary": {"repos": 2, "failed": 0, "clean": 0, "worktrees": 2, "branches": 5,
              "removable_worktrees": 1, "removable_branches": 3, "keep_worktrees": 1, "keep_branches": 2,
              "unmerged": 2, "free_bytes": 22, "unmeasured": 0},
  "repos": [{"root": "...", "default_branch": "main", "note": "", "error": "", "mess": 5, "clean": false,
             "worktrees": [...], "branches": [...], "health": {...}}]
}
```

`summary` 就是文字报告开头的总览：`repos`（扫了几个）、`failed`（扫描失败）、`clean`（干净）、`worktrees`/`branches`（附加 worktree、本地分支总数）、`removable_*`/`keep_*`（可删、留着各几个）、`unmerged`（留着的项里有未合并内容的）、`free_bytes`（可删 worktree 的目录大小之和）、`unmeasured`（可删 worktree 里没量到大小的个数）。

`repos[]` 和文字报告同一个顺序（乱的在前），每个仓库：

| 字段 | 说明 |
|---|---|
| `root` | 主工作区的绝对路径 |
| `default_branch` | 默认分支；找不到为 `""` |
| `note` | 只列不判时的说明（裸仓库、找不到默认分支） |
| `error` | 整个仓库没扫成时的错误；成功为 `""` |
| `worktrees` | 附加 worktree（不含主工作区），每项见下表 |
| `branches` | 本地分支（不含默认分支），每项见下表 |
| `mess` | 乱的程度：可删项数 + 留着的未合并项数 |
| `clean` | 干净（扫成功、判定过、`mess` 为 0），文字报告里折成一行 |
| `health` | 体检，见下表；没体检时为 `null` |

`worktrees[]`、`branches[]` 每项：

| 字段 | 说明 |
|---|---|
| `repo` | 所在仓库，同 `root` |
| `kind` | `worktree` 或 `branch` |
| `name` | 分支名，或 worktree 的绝对路径 |
| `branch` | 检出或对应的分支；worktree 是分离头指针时为 `""` |
| `head` | 完整提交号 |
| `verdict` | 结论：`removable`（可删）或 `keep`（留着） |
| `basis` | 依据：`merged`（内容已在默认分支）、`pushed`（已推到远程）、`prunable`（worktree 目录已不在，只剩登记）、`keep` |
| `evidence` | 可删时的证据，和文字报告同一句；留着时为 `""` |
| `reasons` | 留着的原因，每条一句；可删时为 `[]` |
| `unmerged` | 留着，且有不在默认分支、也没推到远程的内容（独有提交或未提交改动） |
| `last_active` | 最后活动时间（分支取末端提交时间；worktree 取 HEAD 提交、index、reflog 三者最新） |
| `remote`、`remote_ref` | `basis` 为 `pushed` 时：远程名和远程上的引用，其余不出现 |
| `size_bytes` | 只有 worktree 有：目录大小（字节，按文件大小累加）；目录已不在时不出现 |

`health`：

| 字段 | 说明 |
|---|---|
| `note` | 整个跳过时的说明（如裸仓库），此时其余字段为零值 |
| `git_dir_bytes`、`main_bytes` | `.git` 和主工作区（不含 `.git` 和放在里面的附加 worktree）的大小 |
| `branch`、`upstream`、`upstream_gone`、`ahead`、`behind` | 主工作区当前分支（分离头指针为 `""`）、上游（没设为 `""`）、上游是否已不在、领先/落后提交数 |
| `changes`、`untracked`、`stashes` | 未提交改动数（含未跟踪）、其中未跟踪的数、stash 数 |
| `last_commit`、`last_commit_branch` | 本地分支里最新的提交时间和分支名 |
| `backup` | `present`（有没有 backup 远程）、`online`（这次是否联网查）、`online_error`（联不上的原因）、`checked`（比对了几个分支）、`stale[]`（没推到最新的分支：`branch`、`state` 为 `not-pushed`/`behind`/`diverged`/`unknown`、`local`、`remote`、`local_only`、`remote_only`） |
| `errors` | 没查成的项，每条一句 |

## 删除与恢复

- 删之前重新判定，只删报告时和现在都可删、提交没变的项。
- 靠「已推到远程」可删的，先 `git ls-remote` 联网确认远程现在还包含这个提交（超时 20 秒），确认不了就不删。
- worktree 用 `git worktree remove`（不加 `--force`），分支用 `git branch -D`。
- 每一项删之前把恢复命令写进 `${XDG_STATE_HOME:-~/.local/state}/repo-tidy/apply-<时间>.log`：先跑分支的 `git branch <名字> <提交>`，再跑 worktree 的 `git worktree add`。提交对象在 git gc 清掉之前都在。

## 只删勾选的项

`--apply --select <文件>` 只删文件里列的项，`-` 表示从标准输入读。不给路径时只扫这些项所在的仓库。文件格式和 `--json` 每一项的字段相同，挑出来原样送回即可：

```json
{"items": [{"repo": "/path/to/projects/app", "kind": "branch", "name": "agent/x", "head": "<完整提交号>"}]}
```

列了但这次不删的项各记一条原因：已经不在了、现在判定要留着、提交和勾选时不一样了（`head` 对不上）、分支还检出在一个没一起列出的 worktree 里。其余照「删除与恢复」：删前再判定一次，推到远程的联网确认。

加 `--json` 时输出删除结果（不再输出报告）：

| 字段 | 说明 |
|---|---|
| `version` | 同报告的 `version` |
| `log` | 恢复日志的路径；一项都没删时为 `""` |
| `target` | 打算删几项：给了 `--select` 是列的项数，否则是全部可删项 |
| `deleted` | 实际删了几项 |
| `results[]` | 每项：`repo`、`kind`、`name`、`done`（删了没有）、`message`（做了什么或为什么没做） |
| `errors[]` | 整个仓库出的错：`repo`、`error`（比如删前重新判定失败） |

没删全（`deleted < target`）或有仓库出错时退出码是 1。

## 只读

扫描时所有 git 命令带 `GIT_OPTIONAL_LOCKS=0`，不 fetch、不刷新 index；模拟合并产生的对象写进临时目录。唯一的例外：git 写对象时发现对象已存在，会刷新那个已有对象文件的修改时间（防 gc），内容不变。
