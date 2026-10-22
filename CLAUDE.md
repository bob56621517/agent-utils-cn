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

项目尚处于初始化阶段,尚无源码、构建脚本或 CI 配置。构建、运行、测试(含单测)与 lint 命令在代码落地后补充到本节,不要臆造。

## Agent skills

### 工单系统

工单与规格记录在 GitHub Issues(`origin` 仓库),通过 `gh` CLI 读写。详见 `docs/agents/issue-tracker.md`。

### Triage 标签

沿用五个默认标签:`needs-triage`、`needs-info`、`ready-for-agent`、`ready-for-human`、`wontfix`。详见 `docs/agents/triage-labels.md`。

### 域文档

单上下文布局:根目录 `CONTEXT.md` 加 `docs/adr/`。详见 `docs/agents/domain.md`。
