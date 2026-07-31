# Stele

> 工程知识图谱 + agent 记忆层 — 统一可视化 OpenSpec 变更、Trellis 任务与独立 Superpowers 项目产物，支持语义搜索、知识图谱和自动报告。

**单 Go 二进制 + 嵌入式前端。下载即用。**

---

## 核心能力

| 模块 | 功能 |
|------|------|
| 🚀 **变更仪表盘** | KPI 卡片、变更列表、进度条、OpenSpec + Trellis + Superpowers 多 workspace 聚合 |
| ⌨️ **命令面板** | `Ctrl+K` 模糊搜索所有命令、`?` 快捷键速查、类别分组 |
| 🗺️ **知识图谱** | Cytoscape 力导向图、社区检测、分组染色、节点关系可视化 |
| 📅 **时间线** | Gantt 风格、阶段着色、今日标记线、周末高亮、workspace 分行 |
| 📆 **产品日历** | 季度视图、日期产物热力图、按类型排序、点击跳转 viewer |
| ✅ **聚焦待办** | 今日 / 明日 / 逾期 / 未定分组、优先级、Change/Wiki 关联、右侧焦点编辑 |
| 🔍 **语义搜索** | Ternlight 向量 embedding + cosine 相似度 + 关键词增强 |
| ✓ **文档健康** | 死链检测、孤儿节点、lifecycle gap 规则 |
| 📊 **报告生成** | Wiki 文档驱动的周报/月报、证据引用、分层聚类与历史管理 |
| 💬 **AI 对话** | 流式 Chat, 图谱模式 (注入 2-hop 邻域 + 社区综述) |
| 🖱️ **右键菜单** | 变更卡片 / 最近更新 / 日历产物右键复制路径、打开 |
| 🔗 **分享** | 生成分享链接，可设置过期时间 |
| ⚙️ **设置面板** | Provider / Model / API Base 配置 |
| 🧠 **会话记忆层** | agent 会话摘要入图、会话↔文档关系边、文档页「相关会话」、单入口召回 packet |
| 🤖 **MCP Server** | Streamable HTTP 端点, 13 个 tools 供 AI agent 查询知识图谱、召回上下文与管理待办 |

---

## 键盘快捷键

| 快捷键 | 功能 |
|--------|------|
| `Ctrl+K` | 打开命令面板 |
| `Ctrl+1~8` | 切换视图：变更/图谱/时间线/搜索/最近/文档健康/日历/待办 |
| `Ctrl+B` | 收藏夹开关 |
| `Ctrl+=/-` | 放大/缩小 (50%-200%) |
| `Ctrl+0` | 重置缩放 |
| `Escape` | 关闭面板 / viewer / 收藏夹 |

---

## 架构

```
┌───────────────────────────────────────────────────┐
│  Frontend (React + Vite + Tailwind)               │
│  Carbon Design System tokens (IBM Plex Sans)      │
│  WikiGraph · WikiTimeline · SemanticSearch         │
│  ChangeExplorer · ReportView · LintPanel · Chat   │
└───────────────┬───────────────────────────────────┘
                │ HTTP API
┌───────────────┴───────────────────────────────────┐
│  Go Backend (single binary, embedded frontend)    │
│                                                   │
│  ┌─────────────────────────────────────────────┐  │
│  │  Wiki Engine                                │  │
│  │  scan → links (4 layers) → graph → embed   │  │
│  │  → similarity → Louvain → community labels │  │
│  └─────────────────────────────────────────────┘  │
│                                                   │
│  ┌──────────────┐ ┌──────────┐ ┌──────────────┐  │
│  │ Chat/LLM     │ │ Report   │ │ MCP Server   │  │
│  │ (streaming)  │ │ (weekly/ │ │ (JSON-RPC    │  │
│  │              │ │  monthly)│ │  over HTTP)  │  │
│  └──────────────┘ └──────────┘ └──────────────┘  │
│                                                   │
│  fsnotify watcher → incremental rebuild → SSE     │
└───────────────────────────────────────────────────┘
                │
     ┌──────────┴──────────┐
     │  Ternlight (Bun)    │
     │  @ternlight/base    │
     │  384-dim embedding  │
     └─────────────────────┘
```

---

## 知识图谱

### 数据模型

**10 种组件类型:**

| 类型 | 来源 |
|------|------|
| `change` | OpenSpec `.comet.yaml` / Trellis `task.json` |
| `proposal` | OpenSpec `proposal.md` / Trellis `prd.md` |
| `design` | `design.md` |
| `tasks` | OpenSpec `tasks.md` / Trellis `implement.md` |
| `spec` | OpenSpec `specs/` / Trellis `.trellis/spec/` / Superpowers `docs/superpowers/specs/` |
| `plan` | `plans/` / Superpowers `docs/superpowers/plans/` |
| `artifact` | `artifacts/` / Superpowers `docs/superpowers/artifacts/` |
| `diagram` | `diagrams/` 目录下 |
| `report` | `reports/` / Superpowers `docs/superpowers/reports/` |
| `knowledge` | OpenSpec `knowledge/` / Trellis `.trellis/workspace/` / frontmatter `wiki: true` |

### 4 层边提取

| 层 | 来源 | 置信度 |
|---|------|--------|
| **Metadata** | OpenSpec `.comet.yaml`；Trellis `task.json`、context JSONL；Superpowers 精确 frontmatter 身份 | 最高 |
| **Markdown** | 文件内 `[text](path)` 链接 | 高 |
| **Convention-internal** | OpenSpec/Trellis 工作项内部连线；Superpowers design→plan→execution→verify 精确归组 | 中 |
| **Vector** | Ternlight embedding cosine top-3 (阈值 0.5) | 语义 |

### 社区检测

- 多层加权 Louvain (γ=0.7)：结构边按来源加权 1.0/0.9/0.7，向量边只给 0.1-0.4，社区折叠后迭代重跑
- TF-IDF 主题标签 (取标题中最具区分度的 3 个词，如 `kmc · kms · caller`)
- 只有完全无边的文档标为未归类
- 社区综述页 (LLM 生成, 带缓存)

### 增量更新

- fsnotify 监控各来源的持久目录；Superpowers 仅监控 `docs/superpowers/{specs,plans,artifacts,reports}`
- 2s debounce → 增量更新；Superpowers 产物变化触发来源级完整重建
- 内容哈希 + 输入版本校验的 embedding 缓存（正文变化后自动重算）
- SSE push → 前端自动刷新

---

## 语义搜索

- **后端**: Ternlight (`@ternlight/base`, 7MB, 384 维) 通过 Bun 调用
- **排序**: cosine similarity + 标题关键词 boost (+30%)
- **Fallback**: 向量无结果时自动转标题子串匹配
- **性能**: embedding 缓存命中后 rebuild 4s; 搜索 <300ms

---

## 报告生成

- **统一语料**: 直接使用 Wiki 索引内 `proposal/design/tasks/spec/plan/artifact/report/knowledge` Markdown；日期口径为文档最后更新时间
- **周报**: 关系约束 + 向量/词法聚类，按主题并行生成结构化摘要，再渲染为带 `D<n>` 证据引用的 Markdown
- **月报**: 复用完整落在区间内的周报 `PeriodDigest`，仅为月初/月末和覆盖缺口生成裁剪摘要，再聚类渲染 Swiss-style HTML
- **可追溯性**: 每份报告同时保存 `.manifest.json`，记录文档内容哈希、主题、结构化 claims、覆盖告警、模型与聚类版本
- **降级策略**: embedding 缺失时使用确定性词法聚类；索引未就绪或模型输出无法通过证据校验时返回错误且不落盘
- **历史管理**: 持久化到 `~/.stele/reports/`，支持查看、下载和删除（删除时同步清理 manifest）

---

## MCP Server

Stele 内嵌 MCP (Model Context Protocol) Streamable HTTP 端点, 让 AI agent 查询知识图谱并管理待办。Wiki 工具保持只读；待办写工具仅接受 loopback 请求，并要求 `~/.stele/mcp-write-token` 中的 Bearer token。

**端点**: `POST http://localhost:8989/mcp`

| Tool | 说明 |
|------|------|
| `wiki_search` | 语义搜索工程文档 |
| `wiki_component` | 查看组件详情 + 引用关系 |
| `wiki_neighbors` | 2-hop 图谱邻居 |
| `wiki_overview` | 主题社区综述 |
| `wiki_read` | 读取文档内容 |
| `wiki_lint` | 文档健康检查 |
| `wiki_context` | **动手前的单入口召回**：相关文档 + 动过它们的 agent 会话（含意图摘要）+ 命中的 agent 记忆产物，返回紧凑 Markdown |
| `wiki_sessions` | 列出 agent 会话摘要（工具调用统计、读/改过的文档、意图）；不返回会话原始记录 |
| `todo_list` | 按状态、workspace、Change 或关键词筛选待办 |
| `todo_create` | 创建待办（loopback + Bearer） |
| `todo_update` | 更新或清空待办字段（loopback + Bearer） |
| `todo_delete` | 按 ID 删除单条待办（loopback + Bearer） |
| `todo_sync_omp` | 原子同步 OMP 会话完整 Todo 快照，支持 upsert/reconcile 与单调序列防回滚（loopback + Bearer） |

**Agent 配置示例**（`mcp.json`）。只读的 wiki 工具无需鉴权；待办写工具要求 Bearer token，
缺少 `Authorization` 头时它们会返回 `write access denied`：
```json
{
  "stele": {
    "type": "http",
    "url": "http://localhost:8989/mcp",
    "headers": {
      "Authorization": "Bearer <~/.stele/mcp-write-token 的内容>"
    }
  }
}
```

---

## 快速开始

### 安装

```bash
# 克隆
git clone https://github.com/sudashannon/stele.git
cd stele

# 安装 embedding 依赖
bun install

# 构建
cd web && npm install && npx vite build && cd ..
go build -o stele .
```

### 运行

```bash
./stele --port 8989 --dir /path/to/openspec-or-trellis-or-superpowers-project
```

浏览器打开 `http://localhost:8989`

会话记忆层默认读取 `~/.omp/agent/sessions` 下的 agent 会话记录，只保留摘要——标题、工具调用统计、意图，以及
**产出文档**（`write` 新建/覆盖）/ **改动文档**（`edit` 打补丁）/ **读取文档**三组路径——并把「会话 → 文档」关系挂进图谱。
会话记录本身既不进 embedding、不进语义搜索、不进社区聚类，也**永不进知识镜像仓**。
增量方式为 60s 轮询 + 按字节偏移 tail 续读，变更后经 SSE `sessions-updated` 推送，文档页「相关会话」与会话详情就地刷新。
目录不存在时该层自动关闭；用 `--sessions-dir` 指定其它位置：

```bash
./stele --dir /path/to/openspec --sessions-dir ~/.omp/agent/sessions
```

### Systemd 服务

```bash
cp stele.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now stele
```

### 配置 Workspace

通过 UI 添加, 或直接编辑 `~/.stele/workspaces.yaml`:

```yaml
workspaces:
  - alias: miao
    path: /home/user/workspace/miao/openspec
    color: '#0063f8'
    type: openspec
  - alias: trellis-app
    path: /home/user/workspace/trellis-app
    color: '#10b981'
    type: trellis
  - alias: ideas
    path: /home/user/workspace/ideas
    color: '#8b5cf6'
    type: superpowers
```

`type` 可省略，服务会按 `OpenSpec → Trellis → Superpowers` 的优先级自动检测。OpenSpec 可注册 `openspec/` 本身或项目根目录；Trellis 必须注册包含 `.trellis/` 的项目根目录；Superpowers 必须注册仅拥有该来源的项目根目录，并至少包含一个 `docs/superpowers/{specs,plans,artifacts,reports}` 持久目录。混合仓库中的 `docs/superpowers` 仍归 OpenSpec/Trellis 所有，不会重复注册为独立来源；越界符号链接和 `docs/superpowers` 子目录注册会被拒绝。

Trellis 任务状态映射为 `planning → in_progress → completed/rejected`。面板只读取持久文件；“开始执行”和“完成并归档”分别调用项目内 `.trellis/scripts/task.py start` 与 `archive --no-commit`，不会直接改写 `task.json`。

独立 Superpowers 来源为只读：按精确 metadata、`design-doc` 引用或标准文件名归组，展示 `design → plan → build → verify → completed` 生命周期、计划 checkbox 进度和验证报告结果；Stele 不写回这些 Markdown，也不提供迁移按钮。

### 配置 LLM Provider

UI 设置面板, 或编辑 `~/.stele/config.json`:

```json
{
  "active_provider": "minimax",
  "providers": {
    "minimax": {
      "api_key": "sk-...",
      "api_base": "https://api.minimaxi.com",
      "model": "MiniMax-M2.5",
      "temperature": 1,
      "max_tokens": 4096
    }
  }
}
```

---

## 知识产出归档

Agent 产出的文档放到 workspace 的 `knowledge/` 目录即可被自动索引:

```markdown
---
title: Orin INT8 量化调研
tags: [orin, quantization]
---

# 正文...
```

不在 `knowledge/` 目录的文件, 加 `wiki: true` frontmatter 也可以被追踪:

```markdown
---
title: 架构决策记录
wiki: true
tags: [architecture, decision]
---
```

---

## API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/workspaces` | GET/POST | 管理 workspace |
| `/api/changes` | GET | 变更列表 |
| `/api/changes/:name` | GET | 变更详情 |
| `/api/todos` | GET/POST | 筛选、计数与创建待办；LAN GET 只读 |
| `/api/todos/:id` | PATCH/DELETE | 更新或删除单条待办；仅允许 loopback + same-origin |
| `/api/artifact` | GET | 读取文档内容 |
| `/api/chat/message` | POST | AI 对话 (流式) |
| `/api/chat/config` | GET/PUT | Chat 配置 |
| `/api/report` | POST | 从 Wiki 文档生成报告，返回覆盖率、输入文档数、主题数与复用周报 ID |
| `/api/reports` | GET | 报告历史 |
| `/api/reports/get` | GET/DELETE | 查看/删除报告 |
| `/api/wiki/graph` | GET | 完整图谱数据 |
| `/api/wiki/index` | GET | 组件索引 |
| `/api/wiki/component/:id` | GET | 组件详情 + 引用 |
| `/api/wiki/search-semantic` | POST | 语义搜索 |
| `/api/wiki/rebuild` | POST | 重建索引 |
| `/api/wiki/lint` | GET | Lint 问题 |
| `/api/wiki/overview` | GET | 社区综述 |
| `/api/wiki/recent` | GET | 最近更新 (支持 ?offset=&limit=) |
| `/api/wiki/calendar/month` | GET | 日历月视图 (?year=&month=) |
| `/api/wiki/calendar/day` | GET | 日历日视图 (?date=) |
| `/api/wiki/events` | GET (SSE) | 图谱、待办与会话实时更新推送 |
| `/api/wiki/sessions` | GET | agent 会话摘要列表 |
| `/api/wiki/session` | GET | 单个会话摘要 (?id=会话记录路径) |
| `/api/wiki/sessions/refresh` | POST | 立即重扫会话记录并重新挂图 |
| `/api/wiki/context` | GET | 召回 packet (?q=&limit=) |
| `/mcp` | POST | MCP JSON-RPC 端点（8 个 Wiki 工具 + 5 个待办工具） |

---

## 技术栈

| 层 | 技术 |
|---|------|
| 后端 | Go 1.22+, 单二进制 |
| 前端 | React 18, Vite, Tailwind CSS, Cytoscape.js |
| 设计 | IBM Carbon Design System tokens, IBM Plex Sans 字体 |
| Embedding | Ternlight (@ternlight/base, Bun runtime) |
| 图算法 | 多层加权 Louvain 社区检测, TF-IDF 标签, Cosine similarity |
| 文件监控 | fsnotify |
| LLM | MiniMax / Anthropic / OpenAI (可配置) |
| 协议 | MCP Streamable HTTP (JSON-RPC 2.0) |
| 实时推送 | Server-Sent Events (SSE) |

---

## License

MIT
