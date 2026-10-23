# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 语言约定

- 文档、代码注释、提交信息、PR/MR 描述使用中文。
- 由英文模板生成的文件(README 模板、Issue/PR 模板、注释模板等)落地前翻译为中文。
- 仅当符号含义强于语言含义时保留原文:标识符、关键字、协议与格式名、配置键、命令、URL、module path(如 `MCP`、Streamable HTTP、`ghcr.io`、`github.com/bob56621517/deepseek-search-fetch-mcp`)。

## 项目约定

- Go 项目,module 为 `github.com/bob56621517/deepseek-search-fetch-mcp`。
- MCP server,使用官方 Go SDK `github.com/modelcontextprotocol/go-sdk`,以 Streamable HTTP 形式暴露。
- 只暴露 DeepSeek 官方的两个工具:`web_ext_search`、`web_fetch`。
- 搜索工具支持包装层参数 `summary`(默认 `false`):`false` 返回 `snippet`,`true` 返回 `summary`;两个字段始终存在,同一条结果只有一个非空。
- 搜索 `count` 默认 10;低于 5 自动按 5 处理;超过 50 报错。
- 搜索结果不返回 `content` 与 `site_icon`;网页正文统一通过 `web_fetch` 获取。
- 接口契约以 `api/openapi.yaml` 为准。
- 唯一凭据是 DeepSeek API Key。
- 默认监听端口 `18081`。
- 源码发布到 GitHub;容器镜像发布到 GHCR。
- `origin`(GitHub)是主远端;`gitlab` 为备用远端,不是默认推送目标。需要两端一致时使用仓库内已配置的 `git sync` 别名。

## 配置与日志

固定配置文件:`~/.deepseek_search_fetch/config.json`

```json
{
  "deepseek_api_key": "sk-...",
  "deepseek_api_url": "https://api.deepseek.com",
  "server_host": "127.0.0.1",
  "server_port": 18081,
  "request_timeout": "30s",
  "http_max_idle_conns": 5,
  "http_max_idle_conns_per_host": 5,
  "http_idle_conn_timeout": "90s",
  "http_tls_handshake_timeout": "10s",
  "reset_connection_on_error": true
}
```

首次启动时如果目录或文件不存在,会自动创建(Key 为空)。配置只在启动时读取一次,修改后需要重启。配置优先级:**启动参数 > 环境变量 > 配置文件 > 默认值**。

- 启动参数:`--deepseek-api-key`、`--deepseek-api-url`、`--host`、`--port`
- 环境变量:`DEEPSEEK_API_KEY`、`DEEPSEEK_API_URL`、`DEEPSEEK_SEARCH_FETCH_HOST`、`DEEPSEEK_SEARCH_FETCH_PORT`
- 配置文件:`deepseek_api_key`、`deepseek_api_url`、`server_host`、`server_port`、`request_timeout`、`http_max_idle_conns`、`http_max_idle_conns_per_host`、`http_idle_conn_timeout`、`http_tls_handshake_timeout`、`reset_connection_on_error`
- `reset_connection_on_error` 为 true 时,任何上游调用失败都会丢弃空闲连接,下次调用重新建连
- 日志目录:`~/.deepseek_search_fetch/logs/`,文件 `server.log`
- 滚动策略:单文件 10 MiB,最多保留 5 个文件(当前 `server.log` + 4 个备份);日志同时输出到 stderr

## 构建与测试

需要 Go 1.25 或更高版本。

- 构建:`go build ./...`
- 运行:`go run ./cmd/deepseek-search-fetch-mcp`(默认监听 `127.0.0.1:18081`)
- 静态检查:`go vet ./...`
- 格式化检查:`gofmt -l .`
- 单元/集成测试:`go test ./...`
- 竞态检查(需要 CGO 与 C 编译器):`CGO_ENABLED=1 go test -race ./...`
- 构建镜像:`docker build -t deepseek-search-fetch-mcp .`

版本与构建渠道:

- 本地 `go build` 默认 `version=dev`、`build=local`
- 本地指定版本:`-ldflags "-X main.version=v1.2.3"`,渠道仍为 `local`
- Docker/GHCR:注入 `build=ghcr`,版本取构建参数 `VERSION`
- MCP 握手信息中的版本为 `version+build`,例如 `dev+local`、`v1.2.3+ghcr`

国内网络下拉取依赖需先设置 `GOPROXY=https://goproxy.cn,direct`。

测试只经由 MCP 协议边界断言行为,上游全部使用 `httptest` 假服务,不访问真实网络。真实联调时直接注入环境变量或写入固定配置文件,不要把 Key 提交到仓库。

## 目录结构

代码只分两层:

- `internal/mcp`:MCP 层。装配工具、Streamable HTTP 传输、健康检查。
- `internal/openapi`:OpenAPI 工具层。DeepSeek Search/Fetch 客户端、工具定义、参数校验。
- `cmd/deepseek-search-fetch-mcp`:进程入口、配置解析、滚动日志、信号处理与优雅关闭。

## Agent skills

### 工单系统

工单与规格记录在 GitHub Issues(`origin` 仓库),通过 `gh` CLI 读写。详见 `docs/agents/issue-tracker.md`。

### Triage 标签

沿用五个默认标签:`needs-triage`、`needs-info`、`ready-for-agent`、`ready-for-human`、`wontfix`。详见 `docs/agents/triage-labels.md`。

### 域文档

单上下文布局:根目录 `CONTEXT.md` 加 `docs/adr/`。详见 `docs/agents/domain.md`。
