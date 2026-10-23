# deepseek-search-fetch-mcp

把 DeepSeek 官方的网页搜索与抓取能力暴露为单一 MCP 服务。项目自身不实现检索逻辑,只负责 MCP 协议、工具描述、参数校验与凭据注入。

## Language

## 能力与工具

**Search (搜索)**:
按 query 检索网页,返回结构化结果列表。对应上游 `POST /v1/tools/search`。默认返回 `snippet`;`summary=true` 时返回长摘要,两者互斥。搜索不返回网页正文,正文统一通过 Fetch 获取。
_Avoid_: 检索、查询接口

**Fetch (抓取)**:
抓取单个网页,返回页面标题与 Markdown 正文。对应上游 `POST /v1/tools/fetch`。
_Avoid_: 下载、爬取

**工具 (Tool)**:
MCP 客户端可调用的一项能力。当前固定为 `web_ext_search` 与 `web_fetch`;`web_ext_search` 是扩展搜索,支持站点、语言、时间范围等过滤。
_Avoid_: 函数、接口、API

**上游 (Upstream)**:
DeepSeek 官方 API,Base URL 为 `https://api.deepseek.com`。
_Avoid_: 供应商、渠道、backend

**接口契约 (API contract)**:
`api/openapi.yaml`,定义两个工具的请求与响应结构。代码修改应与之一致。
_Avoid_: 文档、spec 文件

## 调用面

**会话 (Session)**:
stateless 模式下单次 HTTP 请求对应的临时 MCP 会话,不跨请求保留状态。
_Avoid_: 连接、上下文

**错误体 (Error envelope)**:
上游错误响应,OpenAI 兼容格式:`{"error":{"message","type","param","code"}}`。
_Avoid_: 错误码、异常

**配置文件 (Config file)**:
固定路径 `~/.deepseek_search_fetch/config.json`,承载 `deepseek_api_key`、`deepseek_api_url`、`server_host` 与 `server_port`,首次启动时自动创建。
_Avoid_: 设置、环境

## 运行时

**MCP 层**:
负责协议与传输的薄层,对应 `internal/mcp`。不感知具体上游接口。
_Avoid_: 服务层、接入层

**OpenAPI 工具层**:
负责工具定义、参数校验与上游调用,对应 `internal/openapi`。不感知 MCP 传输。
_Avoid_: 业务层、适配层

**连接复位 (Connection reset)**:
上游调用失败后丢弃空闲连接,下次调用重新建连,避免复用处于异常状态的连接。
_Avoid_: 重连、断线重试
