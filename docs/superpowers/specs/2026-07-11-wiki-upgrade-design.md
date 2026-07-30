# comet-panel Wiki 升级设计文档

> 日期: 2026-07-11
> 状态: approved
> 分期: 4 phases, 图谱质量优先

## 1. 背景与动机

comet-panel wiki 子系统当前实现了基于结构引用的知识图谱,但存在以下问题:

1. **pathresolve bug**: `.comet.yaml` 的 `design_doc`/`plan` 路径解析错误,导致 YAML 层边大面积失效,大量 Component 沦为孤立散点
2. **无内部自动连线**: 同一 change 下的 proposal→design→tasks 没有自动建边
3. **无语义关联**: 标题含"安全"的多个 change 因无显式引用而完全孤立
4. **无社区结构**: 图谱只有平铺节点,缺乏主题分组
5. **可视化单一**: 只有力导向图,缺少时间维度
6. **Chat 不感知图谱**: 对话无法回答跨 change 的关系问题

### 同类项目参考

| 项目 | 启发 |
|------|------|
| Zhumeng420/llm_wiki | BM25+向量 RRF 融合、Louvain 社区检测、知识缺口主动发现 |
| tiangong-wiki | SQLite+sqlite-vec 持久化、CLI 查询接口 |
| Oshayr/LLM-Wiki | Cytoscape 多布局、Content Gap Dashboard、RAG Chat |
| Basic Memory | MCP 协议、多项目、语义搜索 |

## 2. 分期策略

**优先级轴**: 图谱质量优先 — 先让图正确,再丰富,最后智能。

```
Phase 1: 图谱基础修复     → 散点消除、边完整性
Phase 2: 语义层 + 社区    → 语义关联、主题分组
Phase 3: 可视化增强       → 时间线、染色、筛选
Phase 4: 智能层           → 综述页生成、Chat 图谱查询
```

## 3. Phase 1: 图谱基础修复

### 3.1 pathresolve root 修复

**根因**: `wiki/index.go` 的 `BuildIndex` 调用 `ExtractYAMLLinks(changeDir, ws.Path)`,`ws.Path` 是 openspec 目录。但 `.comet.yaml` 里的 `design_doc: docs/superpowers/specs/...` 路径是相对于项目根(openspec 的父目录)。

**修法**:
```go
// index.go BuildIndex 中:
projectRoot := filepath.Dir(ws.Path) // openspec/.. = 项目根
edges, err := ExtractYAMLLinks(changeDir, projectRoot)
```

**验证标准**:
- `app-security-cloud-integration` 从 0 条边变为 ≥2 条
- 全图孤立散点减少 >50%

### 3.2 同 change 内部自动连线

新增 `ExtractChangeInternalLinks(changeDir string) []Edge`:
- 扫描 changeDir 下已有文件
- 按约定建边:
  - proposal.md → design.md (kind: "generates")
  - design.md → tasks.md (kind: "generates")
  - tasks.md → specs/*/spec.md (kind: "implements")
- Source = `"convention-internal"`
- 在 `BuildIndex` 中,对每个 changeDir 调用

**验证标准**:
- 含 ≥2 产物的 change 全部形成连通子图(0 internal orphan)

### 3.3 lint 规则扩展

新增规则(在 `lint.go` 中):

| 规则 | 条件 | 严重度 |
|------|------|--------|
| `design-no-plan` | 有 design.md,同 change 无 plan 文件,created_at > 3 天 | warning |
| `tasks-no-artifact` | 有 tasks.md,artifacts 目录为空,created_at > 7 天 | warning |
| `stale-active` | phase != archive 且 created_at > 14 天无文件更新 | info |

**实现约束**:
- 需读取 `.comet.yaml` 获取 `created_at` 和 `phase`
- archive 路径下的 change 跳过 stale 检测

## 4. Phase 2: 语义层 + 社区检测

### 4.1 BM25 相似边

新增 `wiki/similarity.go`:

**算法**:
1. 对所有 Component 构建语料:`Title + 正文前 200 字`
2. 构建倒排索引(term → doc list with TF)
3. 对每个 Component 查询自身语料,取 Top-3 相似(排除自身)
4. BM25 score > θ (初始 θ=0.3,可配) 的建边

**Edge 属性**:
- Kind = `"similar"`
- Source = `"bm25"`
- Score 存入 Edge (新增可选字段 `Weight float64`)

**设计约束**:
- 纯 Go 实现,不引入外部依赖
- 相似边不参与 lint(不算有效引用)
- 500 Components 预估 <1s (倒排+堆排)
- 相似边在前端用虚线渲染,低透明度

### 4.2 Louvain 社区检测

新增 `wiki/community.go`:

**算法**: 多层加权 Louvain
1. 初始每个节点自成社区
2. local moving:每个节点尝试移动到邻居社区,选 modularity gain 最大的
3. 收敛后把每个社区折叠成超节点(intra-community 权重变自环),在新图上重跑 —— 迭代至不再移动
4. 输出 `map[string]int` (Component ID → Community ID)

**输入**: Graph 的所有边(结构边 + 相似边都参与),按来源加权:

| 来源 | 权重 |
|---|---|
| `yaml` | 1.0 |
| `convention-internal` | 0.9 |
| `markdown-link` | 0.7 |
| `slug-match` | 0.4 |
| `vector` (cosine ∈ [0.5,1]) | 线性映射到 0.1-0.4 |

同一对文档若同时存在多种来源的边,取**最强来源**(不累加);死链(端点不在索引内)不参与。

**分辨率**: `CommunityResolution = 0.7`(γ<1 偏向更少更大的社区)

**API 扩展**:
```json
GET /api/wiki/graph → {
  "components": [...],
  "edges": [...],
  "communities": { "component-id": 0, ... }  // 新增
}
```

**约束**:
- 只有**完全无边**的节点归入 community=-1 (未归类);连通的小社区照常保留
- rebuild 时执行,结果缓存在 Graph struct
- `Modularity(g, communities, γ)` 导出,供分区质量前后对比

**线上实测(1453 篇 / 5493 边)**:

| 指标 | 单层无权 | 多层加权 γ=0.7 |
|---|---|---|
| 社区数 | 276 | 26 |
| 未归类 | 184 (12.7%) | 18 (1.2%) |
| 最大社区占比 | 1.86% | 13.97% |
| ≤3 篇的社区 | 118 | 6 |
| 模块度 Q | 0.4520 | 0.8381 |

### 4.3 前端分组布局

- 替换 Cytoscape 布局为 `fcose`(支持 compound node)
- 同 community 节点设相同 parent → 自动分组
- 社区外框染色 + 标签(TF-IDF 最高 term)
- 相似边虚线 + 低 alpha

## 5. Phase 3: 可视化增强

### 5.1 时间线视图

新增 `WikiTimeline.tsx`:

**布局**:
- 横轴 = 时间 (created_at → archived_at 或 now)
- 纵轴 = workspace 分组 (miao / model_deploy / home)
- 每个 change = 水平条,颜色 = 社区色

**交互**:
- 时间范围缩放(拖拽/滚轮)
- hover 显示 title + phase
- 双击打开 MarkdownViewer
- 点击条展开内部产物节点

**视图切换**: SideRail 加入 📅 时间线图标

### 5.2 主题染色

- 复用 Phase 2 community 分组
- 12 色预设色板(ΔE>30 保证区分度)
- 社区标签 = TF-IDF 最高 term
- 力导向图:节点加背景光晕(不覆盖 TYPE_COLORS 的形状/填充)
- 时间线:条带填充色 = 社区色
- 图例(左下角):点击可过滤

### 5.3 筛选控件

统一筛选 state 提升到父组件,WikiGraph 和 WikiTimeline 共享:

| 筛选维度 | 控件 | 默认 |
|---------|------|------|
| workspace | 下拉多选 | 全选 |
| component type | 勾选框 (8种) | 全选 |
| 社区/主题 | 图例点击 | 全选 |
| phase 状态 | radio (active / archived / all) | all |
| 时间范围 | 日期选择器 | 最近 90 天 |

## 6. Phase 4: 智能层

### 6.1 主题综述页生成

新增 `wiki/overview.go`:

**触发**: rebuild 后,社区 ≥3 个 change 且(无缓存 或 成员变化 或 成员 mtime 更新)

**输入**: 社区内所有 Component 的 title + 摘要

**LLM Prompt**:
```
你是工程知识库的综述编辑。根据以下 N 个工程变更的摘要,
写一篇 300 字以内的主题综述(中文):
- 这个主题域包含哪些核心变更
- 它们之间的关系和演进脉络
- 当前整体进展和潜在风险
```

**存储**: `~/.comet-panel/wiki/overviews/<community-hash>.md`

**标注**: 头部 `> ⚠️ 本页由 AI 自动生成,非人工产物,仅供参考导航。`

**约束**:
- 综述页不是 Component,不参与图谱边
- 不参与 lint
- 前端:社区分组标题显示"📝 综述"按钮

### 6.2 Chat 图谱查询能力

**Context Injection (通用)**:
- `POST /api/chat/message` 新增可选参数 `includeGraph: boolean`
- 为 true 时,system prompt 自动注入:
  - 当前 change 的 2-hop 邻域(直接 + 间接相连的 Component title)
  - 所在社区的综述页(如果有)
- 前端 ChatBubble 顶部加"📊 图谱模式"开关(默认开)

**Tool Use (可选,provider 支持时)**:
- 定义 function `queryGraph(query: string)`:
  - 后端做 BM25 搜索,返回 Top-5 相关 Component 的 title + type + workspace
  - LLM 可主动调用来回答跨 change 问题
- 不支持 tool-use 的 provider 降级为纯 context injection

**验证标准**:
- 问"哪些 change 和安全相关"能返回 ≥5 个正确结果
- 问"这个 design 被谁引用了"能利用图谱回答

## 7. 技术约束与非目标

### 约束
- 不引入外部数据库(保持单二进制部署)
- 不引入 Python/Java 运行时
- 不引入 embedding 模型(BM25 即可)
- 不做增量索引(当前秒级重建足够)
- 所有新 Go 模块必须有 table-driven 单元测试

### 非目标
- 外部源 ingest(PDF/URL/飞书) — 不做
- 向量检索 — 不做(BM25 替代)
- 实时协作 / 多用户 — 不做
- 综述页手动编辑 / 版本历史 — 不做

## 8. 验收标准汇总

| Phase | 验收 |
|-------|------|
| 1 | 全图孤立散点 <20%(当前 >60%);lint 报告输出新规则 |
| 2 | 安全类 change 自动聚成一个社区;BM25 边 + Louvain 分组布局可见 |
| 3 | 时间线视图可用;社区染色 + 图例;筛选控件 work |
| 4 | 综述页可点击查看;Chat 图谱模式能回答跨 change 问题 |

## 9. 文件变更预估

| Phase | 新增文件 | 修改文件 |
|-------|---------|---------|
| 1 | — | pathresolve.go, index.go, links.go, lint.go |
| 2 | similarity.go, community.go | index.go, api.go, graph.go, WikiGraph.tsx, types.ts |
| 3 | WikiTimeline.tsx, FilterContext.tsx | App.tsx, SideRail, WikiGraph.tsx |
| 4 | overview.go | api.go, handler.go, ChatBubble.tsx, types.ts |
