# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 语言约定

- 文档、代码注释、提交信息、PR/MR 描述使用中文。
- 由英文模板生成的文件(README 模板、Issue/PR 模板、注释模板等)落地前翻译为中文。
- 仅当符号含义强于语言含义时保留原文:标识符、关键字、协议与格式名、配置键、命令、URL、module path(如 `MCP`、Streamable HTTP、`ghcr.io`、`github.com/sxyr/agent-utils-cn`)。

## 项目约定

- Go 项目,module 为 `github.com/sxyr/agent-utils-cn`。
- MCP server,使用官方 Go SDK `github.com/modelcontextprotocol/go-sdk`,以 Streamable HTTP 形式暴露。
- 默认监听端口 `18081`。
- 可注入的配置项支持三种来源:启动参数、环境变量、配置文件,按 **启动参数 > 环境变量 > 配置文件** 的优先级解析。
- 源码发布到 GitHub;容器镜像发布到 GHCR。
- `origin`(GitHub)是主远端;`gitlab` 为备用远端,不是默认推送目标。需要两端一致时使用仓库内已配置的 `git sync` 别名。

## 构建与测试

需要 Go 1.25 或更高版本（SDK 的最低要求）。

- 构建:`go build ./...`
- 运行:`go run ./cmd/agent-utils-cn`(默认监听 `127.0.0.1:18081`)
- 静态检查:`go vet ./...`
- 格式化检查:`gofmt -l .`
- 单元/集成测试:`go test ./...`
- 竞态检查(需要 CGO 与 C 编译器):`CGO_ENABLED=1 go test -race ./...`
- 构建镜像:`docker build -t agent-utils-cn .`

国内网络下拉取依赖需先设置 `GOPROXY=https://goproxy.cn,direct`。

测试只经由 MCP 协议边界断言行为,上游全部使用 `httptest` 假服务,不访问真实网络。
真实上游联调时直接注入环境变量,不要把它们写进配置文件或提交到仓库。

## 目录结构

- `cmd/agent-utils-cn`:进程入口、信号处理与优雅关闭。
- `internal/config`:启动参数、环境变量、YAML 三层配置解析。
- `internal/provider`:供应商定义与透传供应商的 MCP 客户端。
- `internal/filter`:`includes` / `excludes` 解析与可见性计算。
- `internal/upstream`:DeepSeek、博查的 HTTP 调用、兜底判定与错误解析。
- `internal/tools`:四个固定工具的定义与处理器;工具描述集中在本包 `definitions.go`。
- `internal/server`:MCP 服务装配、按请求生成工具面、健康检查。
- `internal/version`:可通过链接器注入的版本号。

## Agent skills

### 工单系统

工单与规格记录在 GitHub Issues(`origin` 仓库),通过 `gh` CLI 读写。详见 `docs/agents/issue-tracker.md`。

### Triage 标签

沿用五个默认标签:`needs-triage`、`needs-info`、`ready-for-agent`、`ready-for-human`、`wontfix`。详见 `docs/agents/triage-labels.md`。

### 域文档

单上下文布局:根目录 `CONTEXT.md` 加 `docs/adr/`。详见 `docs/agents/domain.md`。
