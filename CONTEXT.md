# agent-utils-cn

把多个外部供应商的搜索与抓取能力,以单一 MCP 服务的形式转发给 Agent 的项目。它自身不实现检索逻辑,只负责暴露工具、注入凭据、按请求收窄工具面。

## Language

## 供应商与工具

**供应商 (Provider)**:
提供上游能力的外部服务方,同时是过滤参数中可整组引用的名字:供应商名为 `deepseek`、`bocha`、`context7`、`grep_app`。
_Avoid_: 渠道、来源、服务商、backend

**工具 (Tool)**:
MCP 客户端可调用的一项能力,分固定工具与透传工具两类。
_Avoid_: 函数、接口、API

**固定工具 (Fixed tool)**:
名称、描述与参数由本项目定义的四个工具:`bocha_search`、`bocha_ai_search`、`ds_search`、`ds_fetch`。
_Avoid_: 自建工具、本地工具

**透传工具 (Pass-through tool)**:
名称、描述与参数均取自上游 MCP 端点的工具,来自 `context7`、`grep_app` 两个供应商。
_Avoid_: 动态工具、代理工具

**主供应商 / 备用供应商 (Primary / Fallback provider)**:
`ds_search` 与 `ds_fetch` 优先使用 `deepseek`,失败后由 `bocha` 接替;其余工具只有单一供应商。
_Avoid_: 主库、副库

**兜底 (Fallback)**:
主供应商调用失败后,改由备用供应商提供同一能力的过程。兜底成功时正文与直连无差别,实际供应商通过 `_meta.source` 标注。
_Avoid_: 重试、降级

## 调用面

**可用供应商 (Available provider)**:
凭据已配置、且未被 `excludes` 排除的供应商。工具只有在至少一个可用供应商支撑时才可见。
_Avoid_: 活跃来源、已配置来源

**过滤参数 (Filter parameters)**:
MCP 端点 URL 上的 `includes` 与 `excludes`,用于收窄本次请求可见的工具与可用供应商。`includes` 出现即进入白名单模式;`excludes` 优先于 `includes`。
_Avoid_: 白名单、黑名单、权限

**会话 (Session)**:
stateless 模式下单次 HTTP 请求对应的临时 MCP 会话,不跨请求保留状态。
_Avoid_: 连接、上下文
