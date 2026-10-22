# 工单系统:GitHub

本仓库的工单与规格以 GitHub Issues 形式存放。所有操作使用 `gh` CLI。

## 约定

- **创建工单**:`gh issue create --title "..." --body "..."`。多行正文使用 heredoc。
- **读取工单**:`gh issue view <number> --comments`,用 `jq` 过滤评论,同时获取标签。
- **列出工单**:`gh issue list --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'`,配合适当的 `--label` 与 `--state` 过滤。
- **评论工单**:`gh issue comment <number> --body "..."`
- **添加 / 移除标签**:`gh issue edit <number> --add-label "..."` / `--remove-label "..."`
- **关闭**:`gh issue close <number> --comment "..."`

仓库信息由 `git remote -v` 推断;在克隆目录内运行 `gh` 时会自动识别。

## 将 PR 作为 triage 入口

**将 PR 作为请求入口:否。** _(若本仓库将外部 PR 视为功能请求,改为 `yes`;`/triage` 会读取此开关。)_

设为 `yes` 时,PR 与工单走同一套标签与状态,使用对应的 `gh pr` 命令:

- **读取 PR**:`gh pr view <number> --comments`,差异用 `gh pr diff <number>`。
- **列出待 triage 的外部 PR**:`gh pr list --state open --json number,title,body,labels,author,authorAssociation,comments`,仅保留 `authorAssociation` 为 `CONTRIBUTOR`、`FIRST_TIME_CONTRIBUTOR` 或 `NONE` 的条目(排除 `OWNER` / `MEMBER` / `COLLABORATOR`)。
- **评论 / 打标签 / 关闭**:`gh pr comment`、`gh pr edit --add-label` / `--remove-label`、`gh pr close`。

GitHub 的 issue 与 PR 共享编号空间,裸 `#42` 可能是两者之一:先用 `gh pr view 42` 解析,失败再回退到 `gh issue view 42`。

## 当某个 skill 说"发布到工单系统"

创建一条 GitHub issue。

## 当某个 skill 说"获取相关工单"

运行 `gh issue view <number> --comments`。

## Wayfinding 操作

供 `/wayfinder` 使用。**地图**(map)是一条承载 Notes / Decisions-so-far / Fog 正文的工单,**子工单**是票据。

- **地图**:一条带 `wayfinder:map` 标签的工单,正文保存 Notes / 目前已定决策 / 迷雾。`gh issue create --label wayfinder:map`。
- **子工单**:作为 GitHub sub-issue 关联到地图的工单(对 sub-issues 端点调用 `gh api`)。未启用 sub-issues 时,把子工单加入地图正文的任务列表,并在子工单正文顶部写 `Part of #<map>`。标签:`wayfinder:<type>`(`research` / `prototype` / `grilling` / `task`)。认领后指派给执行的开发者。
- **阻塞关系**:GitHub **原生 issue 依赖**,这是规范且 UI 可见的表示。用 `gh api --method POST repos/<owner>/<repo>/issues/<child>/dependencies/blocked_by -F issue_id=<blocker-db-id>` 添加边,其中 `<blocker-db-id>` 是阻塞者的数字**数据库 id**(`gh api repos/<owner>/<repo>/issues/<n> --jq .id`,不是 `#number` 也不是 `node_id`)。GitHub 通过 `issue_dependencies_summary.blocked_by` 报告(仅未关闭的阻塞者,即实时闸门)。依赖功能不可用时,回退为在子工单正文顶部写 `Blocked by: #<n>, #<n>`。所有阻塞者关闭后,该工单才算解除阻塞。
- **前沿查询**:列出地图下未关闭的子工单(`gh issue list --state open`,范围限定为地图的 sub-issues / 任务列表),剔除存在未关闭阻塞者的(`issue_dependencies_summary.blocked_by > 0`,或 `Blocked by` 行中有未关闭工单)以及已有指派者的;按地图顺序取第一个。
- **认领**:`gh issue edit <n> --add-assignee @me`,这是本次会话的第一次写入。
- **解决**:`gh issue comment <n> --body "<answer>"`,然后 `gh issue close <n>`,再把上下文指针(gist + 链接)追加到地图的"目前已定决策"。
