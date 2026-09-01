---
title: Comet Wiki Claims 经验层扩展
tags: [comet-panel, wiki, mcp, claims, design]
---

# Comet Wiki Claims 经验层扩展（实施任务）

## 1. 背景与目标

Comet Wiki 目前索引的是**文档实体**（components：md 文件、OpenSpec change、目录），语义搜索覆盖文档内容。但它缺少一类重要知识：**经验性断言（claims）**——agent 会话中得出的决策、约束、踩坑、风险。这类知识从未写进文档，是 Greplica 这类工具的核心价值，也是本扩展要补齐的能力。

目标：给 Comet Wiki 增加 claims 层，复用现有 embedding 检索与 MCP 鉴权基建，提供：

1. `wiki_claim_upsert`（写，Bearer 鉴权）：按 id 幂等写入/更新 claim
2. `wiki_claim_query`（读）：语义检索 claims
3. `wiki_claim_list`（读）：按 workspace/kind 过滤列出
4. claims 向量并入现有语义检索候选集（`wiki_search` 与 `mcpWikiSearch` 可命中 claims）

二期（本任务不做，文档保留接口空间）：proposal 审核流（validate/apply JSON 批量提案）、OMP session 自动导入。

## 2. 数据模型

### 2.1 Claim 结构

```go
type Claim struct {
    ID           string    `json:"id"`                     // "claim.<slug>"，稳定幂等键
    Workspace    string    `json:"workspace"`              // 与现有 workspace 命名一致
    Kind         string    `json:"kind"`                   // fact|decision|requirement|task|question|risk
    Truth        string    `json:"truth"`                  // code_verified|source_verified|unknown
    Intent       string    `json:"intent"`                 // intended|accidental|unknown
    Text         string    `json:"text"`                   // 断言正文（窄、可复用，一条一个事实）
    CodeAnchors  []string  `json:"code_anchors,omitempty"` // 文件路径（相对 workspace 根），用于导航
    EvidenceRefs []string  `json:"evidence_refs,omitempty"`// 溯源：session ref 或文档路径
    Tags         []string  `json:"tags,omitempty"`
    CreatedAt    string    `json:"createdAt"`
    UpdatedAt    string    `json:"updatedAt"`
}
```

语义分级（取自 Greplica，保持一致便于工具迁移）：

| 字段 | 取值 | 含义 |
|---|---|---|
| `kind` | `fact` / `decision` / `requirement` / `task` / `question` / `risk` | 断言类型 |
| `truth` | `code_verified`（代码确认）/ `source_verified`（文档/会话） / `unknown` | 可信度 |
| `intent` | `intended` / `accidental` / `unknown` | 是否有意为之 |

约束：`kind/truth/intent` 枚举校验失败即拒绝写入；`Text` 非空；`ID` 匹配 `^claim\.[a-z0-9._-]+$`；同一 `Workspace + ID` 重复写入为 upsert（覆盖 Text 等字段），不产生重复行。

### 2.2 持久化

仿 `internal/todo` 模式（JSON 文件 + schema 版本）：

- 新包 `internal/claims`：`claim.go`（类型 + 校验）、`store.go`（Store 实现）
- 存储位置：`<dataDir>/claims.json`（`dataDir` 与 todo 相同来源，见 `todo.StorePath()`；给 claims 加 `ClaimsStorePath()` 返回同目录 `claims.json`）
- 文件 schema v1：

```json
{
  "schemaVersion": 1,
  "claims": [ ...Claim 数组... ]
}
```

- 迁移策略：`schemaVersion != 1` 时拒绝加载（仿 todo 的 v1/v2/未来拒绝模式）；未来升级时按 todo schema v2 的迁移先例处理
- Store 线程安全：`sync.RWMutex`，写操作原子替换文件（先写临时文件再 rename，仿 todo store）

### 2.3 Embedding

- claim 的 embedding 输入：`Component{ID: "claim:"+id, Title: Text 截断前 120 字符, Path: ""}`（`Type` 用 `TypeKnowledge` 或新增 `TypeClaim`——建议新增 `TypeClaim = "claim"`，避免污染文档类型计数）
- 调用现有 `wiki.ComputeEmbeddings(components, scriptPath)`（Bun `scripts/embed.ts`，384 维）
- 向量存放：**独立 map**（`claimsVectors map[string][]float32`，挂在 API 结构上，`sync.RWMutex` 保护），不并入 graph.embeddings（避免影响社区检测/向量边/文档相似度等现有聚合逻辑）
- 写入时机：`wiki_claim_upsert` 成功落盘后立即为变更的 claim 计算向量；失败时容忍（后续查询时按需补算或返回无结果）
- 向量缓存：仿 `embeddings.bin` 的 `EmbeddingEntry`（ContentHash + InputVersion）加 `claims-embeddings.bin`，key 为 claim id。ContentHash 用 `sha256(Text)`，InputVersion 复用 `EmbeddingInputVersion`

## 3. MCP 工具契约

### 3.1 注册

在 `wiki/mcp.go` 的 `mcpToolsCall` switch 增加三个 case；`mcpToolsList` 增加对应工具描述（仿 todo_* 的 description 风格）。

### 3.2 wiki_claim_upsert（写，需鉴权）

- 入参：`{workspace: string, claims: Claim[]}`（一次 1..N 条）
- 鉴权：`mcpAuth(r, token)`，失败返回 `write access denied: loopback + Bearer token required`（与 todo 一致）
- 校验：逐条校验枚举/Text/ID，任一非法 → 整体拒绝（400 风格错误结果），不部分写入
- workspace 合法性：必须存在于 `workspaces.yaml` 注册列表（仿 todo 的 workspace 校验）
- 返回 JSON：`{applied: number, claims: Claim[]}`（applied = 实际写入条数；claims = 该 workspace 当前全部 claims，便于客户端验证）
- 幂等：同 id 重写 = 覆盖；`UpdatedAt` 刷新，`CreatedAt` 保留

### 3.3 wiki_claim_query（读，无需鉴权）

- 入参：`{query: string, workspace?: string, topK?: number}`（topK 默认 5，上限 20）
- 流程：仿 `mcpWikiSearch`（mcp.go 约 339-389 行）：
  1. `ComputeEmbeddings` 对 query 出向量（失败 → isError 结果）
  2. 遍历 claimsVectors，`cosineSim > 0.15` 的候选
  3. 排序取 topK，同时做 Text 子串命中保底（仿 lexical fallback）
- 返回文本：每条 `[kind/truth] text (workspace, similarity: NN%)` + anchors 列表

### 3.4 wiki_claim_list（读）

- 入参：`{workspace?: string, kind?: string}`（可过滤）
- 返回 JSON：`{claims: Claim[]}`（按 UpdatedAt 倒序）

### 3.5 wiki_search 融合（增强，非必需但推荐）

`mcpWikiSearch` 的候选循环里追加 claims：claim 的 Title（Text 截断）参与 title 匹配，向量对比改用 claimsVectors。保持文档结果优先（claims 排在文档后），避免干扰现有检索行为。**若改动面大，可跳过此条，单独用 wiki_claim_query。**

## 4. 涉及文件清单

| 文件 | 改动 |
|---|---|
| `internal/claims/claim.go` | 新：Claim 类型、枚举校验、`TypeClaim` 常量 |
| `internal/claims/store.go` | 新：Store（load/save/upsert/list、schema v1、RWMutex、原子写） |
| `internal/claims/store_test.go` | 新：schema 拒绝、upsert 幂等、workspace 过滤、原子写 |
| `wiki/mcp.go` | 三个工具注册 + 处理函数；claims 向量 map 挂 API |
| `wiki/mcp_claim_test.go` | 新：鉴权拒绝、非法枚举、upsert 幂等、query 命中（fake bun 仿 `incremental_test.go` 的 `installFakeBun`） |
| `main.go` | 初始化 claims store + 注入 API（仿 todo store 初始化，约 145-150 行） |
| `wiki/embed.go` | 复用，不改（或仅在 ComputeEmbeddings 需要时确认脚本路径） |

## 5. 验收标准

1. `go test ./internal/claims ./wiki` 通过（新测试覆盖：枚举校验、upsert 幂等、鉴权、query 排序）
2. 启动 comet-panel 后：
   - 无 token 调 `wiki_claim_upsert` → `write access denied`
   - 带 token upsert 2 条 claim → `applied: 2`，重复 upsert 同 id → `applied: 1` 且不重复
   - `wiki_claim_query "485 波特率限制"` → 命中对应 claim（text 相关）
   - `wiki_claim_list --workspace rx101` → 只返回该 workspace
3. 现有测试不回归（`go test .`、`go test ./internal/todo ./wiki`）
4. 前端无需改动（纯 MCP 扩展）；`npm run build` 若受影响需验证——预期不受影响

## 6. 参考语义（来自 Greplica，保持一致）

- 一个 claim 只表达一个事实/决策；长内容拆多条
- 经验类默认 `source_verified` + `evidenced_by` 会话 ref；只有代码确认的才用 `code_verified` + `code_anchors`
- 写入原则：`Would this memory change where a future agent looks, what it avoids, or how it interprets this repo next time?` —— 不满足就丢弃
- 会话 ref 格式建议：`omp-session:<sessionId>`（OMP session jsonl 的 `id` 字段，路径 `~/.omp/agent/sessions/` 下可定位）

## 7. 参考实现

- 现有语义检索：`wiki/api.go` `HandleSemanticSearch`（约 389-430 行）、`wiki/mcp.go` `mcpWikiSearch`（约 325-389 行）
- embedding：`wiki/embed.go`（`ComputeEmbeddings` / `EmbeddingEntry` / `EmbeddingInputVersion`）、`wiki/semantic_text.go`（`ExtractSemanticText` 语义投影）
- 写鉴权：`wiki/mcp.go` `mcpAuth`（约 606 行）+ todo 各写工具的调用模式（约 704-710 行）
- todo store 迁移/原子写先例：`internal/todo/store.go`、`internal/todo/todo.go`
- Greplica 原始设计（可参考其 claims/evidenced_by 语义，勿直接复制其实现）：github.com/autoloops/greplica

## 8. 二期预留（本任务不实现）

- `wiki_proposal_validate` / `wiki_proposal_apply`：批量 JSON 提案（compatible with Greplica proposal schema），validate 校验循环引用/锚点
- OMP session 自动导入：`omp-session:<id>` 溯源 + 从 `~/.omp/agent/sessions/` jsonl 提取（格式转换参考 `/tmp/omp2codex.py` 思路：session 行 → session_meta，message 行 role user/assistant → user_message/agent_message，过滤 developer 与工具调用）
- claims 前端展示（TodoPanel 同构）

## 9. 已交付状态（2026-08-25，含 OpenWiki 吸收）

实现落地后与原计划的主要差异（本节为准）：

### 9.1 MCP 工具（实际 3 个，非 3.2-3.4 的 3+1）

| 工具 | 说明 |
|---|---|
| `wiki_claim_upsert` | 写（Bearer 鉴权）。入参 `{workspace, claims[]}`，逐条校验、整体拒绝；status 只接受 `active`/`retracted`，`stale` 由系统自动标记，重新 upsert 同 id 即清除 |
| `wiki_claim_search` | 读。`{query, workspace?, kind?, limit?}`，语义检索 + 文本子串保底（embedding 脚本缺失时自动降级）；原计划的 `wiki_claim_query` 与 `wiki_claim_list` 合并为此一个工具 |
| `wiki_claim_get` | 读。按 id 查看完整 claim，含证据资源与最后验证的版本状态 |

### 9.2 证据版本化（核心新增，原计划没有）

claim 的新鲜度由**版本化证据**驱动（`internal/claims/evidence.go`）：

- 证据 URI 三种：`ws://<ws>/<rel>#L<from>-L<to>`（代码行范围 + 上下各 3 行 context 哈希）、`doc://<ws>/<rel>`（整文件哈希）、`session://<id>`（transcript size+mtime）
- 版本 token 确定性、无模型：`lines-v1:<sha256>` / `doc-v1:<sha256>` / `session-v1:<size>:<mtime>`
- 解析器 `claims.Resolver` 由 API 从当前 workspace 列表构建；`ParseResource` 拒绝绝对路径/越界/`.git`
- 过期原因三态：`version-changed` / `evidence-missing` / `resolution-error`（evidence missing 视为过期而非配置错误）

### 9.3 过期检测闭环

| 触发点 | 行为 |
|---|---|
| watcher 文件变更（`wiki/watcher.go`） | `CheckClaimsForFiles` 只复检证据引用了变更文件的 claim，翻转 stale 时 SSE `claims-updated` 广播 |
| `wiki_lint` / REST `/api/wiki/lint` | `CheckAllClaims` 全量复检（OpenWiki 所说的 scheduled verification/preflight），每条 stale claim 产生 `stale-claim` lint issue |
| `wiki_context` 召回包（`session_api.go`） | `BuildContextPacket` 附带最多 10 条 stale claims（含 evidence 资源），markdown 渲染 "Stale claims (re-verify before relying on these)" |

### 9.4 OKF v0.2 导出（OpenWiki 吸收，`wiki/okf.go` + `wiki/mirror.go`）

镜像仓（knowledge-repo）在每次 flush 时额外投影：

- 每个镜像 `.md` 文档获得 OKF v0.2 合规 frontmatter：`type`（组件类型，作者已有 type 优先）、`generated: {by: stele-wiki, at}`、`sources`（claims 证据投影：active+stale，retracted 排除）、`status: stale`（任一关联 claim 过期）、`verified`（有 claim 且全新鲜）
- 根 `index.md`：声明 `okf_version: "0.2"` + 概念清单；根 `log.md`：追加式更新历史
- 无 projector 时（`SetOKF` 未调用）保持原样逐字拷贝行为

### 9.5 文件清单（实际）

| 文件 | 状态 |
|---|---|
| `internal/claims/{claim,store,evidence}.go` | 新（claim 类型/校验、Store、Resolver+CheckClaim） |
| `internal/claims/claims_test.go` | 新 |
| `wiki/claims_api.go` | 新（store 接线、CheckAllClaims/CheckClaimsForFiles、claimLintIssues、StaleClaimsForDocs） |
| `wiki/claim_vectors.go` | 新（claim 向量缓存，text-hash 绑定） |
| `wiki/mcp_claims.go` | 新（3 个 MCP 工具处理） |
| `wiki/mcp_claim_test.go` | 新（鉴权/幂等/枚举/检索/过期链路） |
| `wiki/okf.go` + `wiki/okf_test.go` | 新（OKF 投影器 + 镜像端到端） |
| `wiki/lint_mermaid.go` + `wiki/lint_mermaid_test.go` | 新（mermaid fence 校验，`mermaid-syntax` lint 规则） |
| `wiki/{api,session_api,mcp,mirror,watcher}.go` | 改（claims 字段/召回包/lint/OKF flush/文件变更复检） |
| `main.go` | 改（claims store 初始化 + `mirror.SetOKF`） |
