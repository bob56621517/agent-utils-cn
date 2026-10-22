# 域文档

工程类 skills 在探索代码库时应如何消费本仓库的域文档。

## 探索之前先读这些

- 仓库根目录的 **`CONTEXT.md`**,或
- 若存在仓库根目录的 **`CONTEXT-MAP.md`**:它指向每个上下文各自的 `CONTEXT.md`,读取与当前主题相关的那些。
- **`docs/adr/`**:读取与你即将改动的领域相关的 ADR。多上下文仓库中,还要检查 `src/<context>/docs/adr/` 下的上下文级决策。

若上述文件不存在,**静默继续**。不要提示缺失,也不要主动建议创建它们。`/domain-modeling` skill(经由 `/grill-with-docs` 与 `/improve-codebase-architecture` 触达)会在术语或决策真正确定时才按需创建。

## 文件结构

单上下文仓库(绝大多数仓库):

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-event-sourced-orders.md
│   └── 0002-postgres-for-write-model.md
└── src/
```

多上下文仓库(根目录存在 `CONTEXT-MAP.md`):

```
/
├── CONTEXT-MAP.md
├── docs/adr/                          ← 系统级决策
└── src/
    ├── ordering/
    │   ├── CONTEXT.md
    │   └── docs/adr/                  ← 上下文级决策
    └── billing/
        ├── CONTEXT.md
        └── docs/adr/
```

## 使用术语表的词汇

当你的产出中提及某个领域概念(工单标题、重构提案、假设、测试名称),使用 `CONTEXT.md` 中定义的术语。不要漂移到术语表明确回避的同义词。

若需要的概念尚未进入术语表,这是一个信号:要么你在发明项目不曾使用的语言(请重新考虑),要么确实存在缺口(记下来,交给 `/domain-modeling`)。

## 标出 ADR 冲突

若你的产出与已有 ADR 相矛盾,要显式指出,而不是悄悄覆盖:

> _与 ADR-0007(事件溯源订单)冲突,但值得重新讨论,因为……_
