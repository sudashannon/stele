---
title: Comet Panel 会话记忆层
tags: [comet-panel, sessions, memory, wiki, design]
---

# Comet Panel 会话记忆层（已实现）

## 1. 为什么不做"把会话索引进去"

实测 `~/.omp/agent/sessions`：74 个 `.jsonl` / 388 MB，单文件最大 157 MB。取一个 30.1 MB 的真实会话逐行统计：

| 项 | 数量 |
|---|---:|
| 总行数 | 3746 |
| message 行 | 2481（user 31 / assistant 1243 / toolResult 1204 / developer 3） |
| toolCall part | 1205 |
| user + assistant 正文 | 62 KB |
| 正文占文件字节 | **0.21%** |

结论：原始记录既不能进图谱也不能进 embedding。同一结论下，OMP 17.2.0 自带的 Autonomous Memory（`memory.backend: local`）已经在做"会话 → 经验文本"的 LLM 蒸馏（两阶段管道 + 密钥脱敏 + `MEMORY.md` / `memory_summary.md` / `skills/`），所以本层**不重造蒸馏**。

## 2. 本层做的是 omp 三个后端都不做的事

`memory.backend` 是封闭枚举 `off | local | hindsight | mnemopi`（无第三方注册点），三者都只产出自然语言片段，都不知道面板里有 1458 个文档实体。本层补的是：

- **机械关系**：会话记录里的 `toolCall.arguments.path`（read）、edit patch 的 `[path#tag]` header、write 的 `path`，与已索引组件求交集 → `session → document` 边。零 LLM。
  - `write`（新建/整体覆盖）与 `edit`（打补丁）在摘要里**分开**成「产出文档 / 改动文档」，因为它们回答的是不同问题；在图里共用 `edits` 边类型（都表示"这个会话改过它"）。
- **跨 workspace 单入口召回**：`wiki_context` 返回"相关文档 + 动过这些文档的会话（含意图摘要）+ 命中的 agent 记忆产物"。

## 3. 数据流

```
~/.omp/agent/sessions/<cwd-slug>/<ISO>_<uuid>.jsonl
        │ 流式逐行解析（tail 续读，按 size+mtime 失效）
        ▼
internal/sessions.Digest ── 缓存 ~/.stele/wiki/sessions.json
        │ cwd 最长前缀匹配注册 workspace（rx101 胜过父级 miao）
        ▼
wiki.SessionsIndex.Apply(graph)  ← 每次 rebuild 前挂到新图上，幂等
        │
        ├─ TypeSession 组件（Path=jsonl，永不读其字节）
        └─ Source="session" 边：reads / edits，Weight=0
```

## 4. 隔离契约（每条都有测试）

| 风险 | 处理 | 位置 |
|---|---|---|
| 记录被推到知识镜像远端 | 镜像跳过 `TypeSession`；`relativeToWorkspace` 逃逸即返回空、失败关闭 | `wiki/mirror.go` |
| 157 MB 进 embedding / Bun stdin | 会话不进 `BuildIndex` 的组件集，因此不进 embedding、不进 vector 边 | 结构性保证 + `wiki/sessions.go` |
| 每次写入触发全量 Rebuild | 不走 `classifyChanges`/watcher；独立索引 + 60s 轮询 + rebuild 前 graft | `main.go` `pollSessions` |
| 未知 Source 落 `edgeWeight` default 0.7 主导 Louvain | `edgeWeight` 首行显式 `SourceSession → 0`；`graphAdjacency` 节点集排除会话 | `wiki/community.go` |
| 语料量变化改动全部 tag 边权重 | `ComputeTagEdges` / `EnrichComponentTags` 过滤会话 | `wiki/tag_edges.go`、`wiki/taxonomy.go` |
| 会话污染文档检索 | `rankSemanticSearch` 跳过 `TypeSession` | `wiki/api.go` |
| 无边会话报 orphan；会话反链把孤儿文档变成 low-link | lint 跳过会话；`documentEdgeCounts` 只数非会话边 | `wiki/lint.go` |
| 内容质量规则 `readBody` 读 157 MB | `lintableBody` 门禁（会话 + archive） | `wiki/lint.go` |
| 前端把会话边画进 Cytoscape / 把 jsonl 塞进 MarkdownViewer | `structuralEdges` 排除 `session`；`App` 按 `type==='session'` 路由到 `SessionDetail` | `web/src/components/WikiGraph.tsx`、`web/src/App.tsx` |

## 5. 线上实测（部署后）

- 74 个会话记录 → **14** 个会话组件（其余 cwd 在注册 workspace 之外，自动丢弃，含 57 个 `/tmp` 丢弃会话）
- **80** 条会话边（reads/edits），weight 全 0
- 图谱 6157 边中 tag 边仍 566 条、权重区间 0.219–0.400（未被会话语料扰动）
- communities 键 1459 = 1473 组件 − 14 会话（会话未进聚类）
- 文档检索 1027 条结果中会话数为 0
- Cytoscape 实际渲染：935 节点 / 898 边，其中会话边 **0**、会话节点 **0**
- 知识镜像仓中 `.jsonl` 数量 **0**

## 6. 召回形态

`wiki_context "PCIe 三通道 通信"` 实际返回：3 篇相关文档（带 tag）+ 2 个动过它们的会话（含各自意图摘要与匹配文档数）。UI 侧：文档页底部「相关会话」→ 点击进入会话详情（工具调用直方图、意图列表、编辑/读取文档清单）→ 点击文档回到正常查看器。

## 7. 与 agent 记忆产物的关系

`~/.omp/agent/memories/<project>/{MEMORY.md, memory_summary.md, skills/*/SKILL.md}` 在 `wiki_context` 查询时**按需读盘**（每文件上限 4 KB，只认这几个文件名），不进索引、不进 embedding、不进镜像——因为 omp 拥有它们的生命周期且会整体重写。本机当前该目录尚未生成（`stage1_outputs` 0 行、5 个 job 卡在 running），此时该段为空，不影响其余功能。

## 8. 本轮明确不做

- claims 写接口：`mnemopi` 后端已提供 `retain`/`recall`/`memory_edit`，再加一套写 API 是第三套记忆入口
- 自建蒸馏 / 自建脱敏管道
- Greplica 的 managed 模式、OIDC 证明链、Memory PR 与证据打包
- 原始记录进图谱、进 embedding、进镜像

## 9. 已知盲区（结构性，不靠猜命令行补）

关系只来自显式带路径的工具参数。实测 14 个会话的工具调用总量里：

| 工具 | 次数 | 是否产生关系 |
|---|---:|---|
| `bash` | 4575 | ❌ heredoc / `cat >` / `sed -i` 建的文件不可见 |
| `read` | 3276 | ✅ 读取 |
| `edit` | 816 | ✅ 改动 |
| `write` | 648 | ✅ 产出 |
| `task` | 131 | ❌ 子 agent 的编辑不在父会话记录里 |

不解析 bash 命令行是有意的：审计已指出误报（把一个会话挂到它从未打开的文件上）比漏报更糟。子 agent 盲区需要 omp 侧持久化子会话记录才能解，不在本层能力范围内。

另有单会话路径上限 `MaxPaths=400`，超出时 `pathsTruncated` 为真，UI 会提示「文档过多，仅显示前若干条」。

## 10. 实时性

- 后端：60s 轮询 + `size+mtime` 失效判定 + **从上次字节偏移 tail 续读**（不重扫 388 MB），rebuild 前也刷一次，变更后广播 SSE `sessions-updated`
- 前端：`useWikiEvents` 支持 `sessions-updated`；文档页的「相关会话」与会话详情卡收到事件后**就地刷新**，不回到 loading 态
- 实测：同一个活跃会话在几次部署间隔里标题从 `Commit and push battery handler fix` → `Deploy updated KMC backend to prod`、轮次 233 → 236，均被增量捕获
