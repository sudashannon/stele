# Comet-Panel：部署指南与功能使用手册

## 一、概述

Comet-Panel 是一个面向个人 AI 协作开发（Vibe Coding）的本地控制面板。它把 Comet 工作流中产生的中间产物与最终产物统一管理，并进一步组织成可搜索、可链接、可被 AI Agent 直接调用的 Wiki / Graph / Search / MCP 知识层。

单 Go 二进制 + 嵌入式前端。下载即用，零外部依赖。

### 核心能力

- 变更仪表盘——KPI 卡片、变更列表、进度条、多 workspace 聚合
- 知识图谱——力导向图、社区检测、节点关系可视化
- 时间线——按时间轴展示变更诞生与归档
- 语义搜索——向量 embedding + cosine 相似度
- Lint——死链检测、孤儿节点、lifecycle gap
- 报告生成——LLM 驱动的周报与月报
- AI 对话——流式 Chat，图谱模式
- MCP Server——供 AI Agent 查询知识图谱

---

## 二、前置依赖

在开始部署之前，确保以下软件已安装：

| 依赖 | 版本要求 | 用途 |
|------|---------|------|
| Go | >= 1.22 | 编译后端 |
| Node.js | >= 20 | 构建前端 & 运行 OpenSpec |
| Bun | >= 1.1 | 运行 embedding 脚本 |
| Systemd（可选）| - | 注册为系统服务 |

验证命令：

```bash
go version
node -v
bun --version
```

---

## 三、安装 OpenSpec 与 Comet（必须）

Comet-Panel 的数据源是 OpenSpec 目录。**必须先安装 OpenSpec 和 Comet。**

### 3.1 安装 OpenSpec

```bash
npm install -g @fission-ai/openspec
openspec --version  # 验证安装
```

### 3.2 初始化 OpenSpec

进入你的工程目录（或使用现有仓库）初始化：

```bash
cd /path/to/your/project
openspec init
```

初始化后会在当前目录生成 `.openspec/` 目录以及 `openspec/` 目录结构。

### 3.3 创建第一个 Change

```bash
openspec change new "first-change"
```

此命令会在 `openspec/changes/first-change/` 下生成 `proposal.md`，你可以编辑它描述变更内容。

### 3.4 安装 Comet Skill

Comet 是 AI Agent 驱动的工作流引擎，以 Agent Skill 形式分发。安装方式取决于你的 Agent 平台：

#### Oh My Pi / OpenCode

```bash
# 安装 Comet Skill
# 对于 Oh My Pi，通过 skills 机制安装
oh-my-pi skills install comet

# 或者手动克隆
git clone https://github.com/oh-my-pi/skills.git /path/to/skills
```

安装后目录结构：

```
/path/to/agents/skills/comet/
├── SKILL.md
├── scripts/
│   ├── comet-state.mjs
│   ├── comet-guard.mjs
│   ├── comet-handoff.mjs
│   ├── comet-archive.mjs
│   ├── comet-intent.mjs
│   └── ...
└── reference/
    └── ...
```

#### 验证 Comet 安装

确保 `comet-env.mjs` 能被定位：

```bash
find ~ -path '*/comet/scripts/comet-env.mjs' -type f 2>/dev/null
```

如果找到路径，说明安装成功。

### 3.5 OpenSpec 与 Comet 关系说明

- **OpenSpec** 负责 WHAT——大纲、提案、spec 生命周期、归档
- **Comet** 负责 HOW——技术设计、计划、执行、收尾
- 两者配合形成双星开发流程：`open → design → build → verify → archive`

### 3.6 标准 Workflow

当你在 Agent 中发起一个变更时，流程通常为：

1. agent 检测现有 Phase
2. 按阶段路由到 `/comet-open` → `/comet-design` → `/comet-build` → `/comet-verify` → `/comet-archive`
3. 每个阶段产出对应的中间产物（proposal.md, design.md, tasks.md, 验证报告等）

---

## 四、从源码部署 Comet-Panel

### 4.1 克隆仓库

```bash
git clone https://github.com/sudashannon/stele.git
cd stele
```

### 4.2 安装前端依赖

```bash
cd web && npm install && cd ..
```

### 4.3 安装 Embedding 依赖

```bash
bun install
```

### 4.4 构建

```bash
cd web && npx vite build && cd ..
go build -o stele .
```

构建完成后会生成一个单文件二进制 `stele`。

### 4.5 首次运行

```bash
# --dir 指向你的 openspec 目录
# --port 服务端口（可选，默认 8989）
./stele --port 8989 --dir /path/to/your/project/openspec
```

浏览器打开 `http://localhost:8989`。首次启动时后端会扫描 openspec 目录并构建知识索引，等待嵌入完成后即可完整使用。

### 4.6 注册 Systemd 服务（可选）

```bash
# 编辑服务文件中的路径和工作目录
cp stele.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now stele
```

服务文件内容参考：

```ini
[Unit]
Description=Stele Dashboard
After=network.target

[Service]
Type=simple
User=your-username
WorkingDirectory=/path/to/stele
ExecStart=/path/to/stele/stele --port 8989 --dir /path/to/openspec
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
```

### 4.7 配置 Workspace

多 workspace 注册。支持两种方式：

**方式一：通过 UI 添加**

在设置面板中的 Workspace 管理区域添加，填写 alias、path、color。

**方式二：直接编辑配置文件**

编辑 `~/.stele/workspaces.yaml`：

```yaml
workspaces:
  - alias: miao
    path: /home/user/workspace/miao/openspec
  - alias: rx101
    path: /home/user/workspace/miao/rx101
    color: '#0063f8'
  - alias: lz100
    path: /home/user/workspace/miao/lz100
    color: '#10b981'
```

### 4.8 配置 LLM Provider

Comet-Panel 的 AI 对话和报告生成需要配置 LLM。支持的方式：

**方式一：UI 设置面板**

打开设置 → Provider，配置 Active Provider、API Key、API Base、Model、Temperature、Max Tokens。

**方式二：编辑配置文件**

编辑 `~/.stele/config.json`：

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
    },
    "claude": {
      "api_key": "sk-ant-...",
      "api_base": "https://api.anthropic.com",
      "model": "claude-sonnet-4-20250514",
      "temperature": 0.7,
      "max_tokens": 8192
    }
  }
}
```

---

## 五、Comet-Panel 功能介绍

### 5.1 变更仪表盘

**入口**：侧边栏首个按钮

这是 Comet-Panel 的主视图。直观展示所有 OpenSpec Change 的状态。

内容：
- **KPI 卡片**——活跃变更数、已归档数、卡死预警数、Verify 失败数、未完成任务数
- **变更列表**——每个 Change 的名称、阶段、流程、任务完成率
- **筛选**——按状态、工作流、阶段、workspace 过滤
- **搜索**——按变更名称搜索

点击一个变更可以查看其完整详情：

- 当前阶段（open / design / build / verify / archive）
- 任务完成度（tasks.md 勾选进度）
- 产物清单（proposal、design doc、plan、spec 等）
- 文档反向引用（其他文档中引用此变更的链接）
- 审查状态

### 5.2 知识图谱

**入口**：侧边栏图谱按钮

基于力导向图展示所有文档之间的关系。

内容：
- **节点类型**——10 种（change / proposal / design / tasks / spec / plan / artifact / diagram / report / knowledge），不同颜色区分
- **边**——4 层链接提取：YAML 引用、Markdown 链接、产物约定、向量语义
- **社区检测**——多层加权 Louvain 自动聚类，每个社区按标题 TF-IDF 自动命名
- **交互**——拖拽、缩放、点击节点打开文档
- **筛选**——按 workspace 或社区过滤

### 5.3 时间线

**入口**：侧边栏时间线按钮

按时间轴展示变更的诞生与归档。每条分支对应一个 workspace，颜色与 workspace 配置一致。

### 5.4 语义搜索

**入口**：侧边栏搜索按钮

支持全文语义搜索。

内容：
- **向量 embedding**——基于 Ternlight（384 维，通过 Bun 调用）
- **排序**——cosine similarity + 标题关键词 boost
- **分页**——支持大量结果
- **结果点击**——直接跳转到对应文档

使用时输入关键词，系统会返回最相关的前若干篇文档及其相似度。

### 5.5 Markdown 查看器

文档正文直接渲染为富文本。

能力：
- Markdown 渲染——GFM 表格、代码块、TOC 导航
- 图表渲染——Mermaid 图（自动降级）、PlantUML 图（Kroki）
- 文档内导航——跳转到章节、页面内 TOC
- 图片放大查看——点击图片可预览
- 资源路径重写——相对路径自动解析到后端 Artifact API
- 产物切换——Change 的多个产物间快速切换
- 收藏——标记感兴趣的文档
- 侧边栏——显示反向引用（哪些其他文档链接到此文档）

### 5.6 Lint 检查

**入口**：侧边栏 Lint 按钮

自动检查文档质量，包括：

- 死链——当前文档中引用的链接无法被解析
- 孤儿节点——没有入边（反向引用）的组件
- lifecycle gap——Change 缺少关键产物

问题列表中的每个条目可直接点击，在 Markdown Viewer 中打开对应来源文件。

### 5.7 最近文档

**入口**：侧边栏最近按钮

展示最近修改的文档列表，方便回溯。

### 5.8 AI 对话

当在 Comet-Panel 中打开任何文档时，右下角会出现 AI 对话气泡。

对话上下文：

- 在 Change 视图中打开文档：自动将该 Change 的所有产物作为上下文
- 在其他视图中打开文档：将当前文档路径作为上下文
- 可手动勾选额外的上下文文件
- 支持图谱模式：自动注入文档在图谱中的 2-hop 邻域 + 社区综述

支持流式输出（SSE），实时展示 AI 回复。

### 5.9 报告生成

**入口**：侧边栏报告按钮

从 Comet Change 数据自动生成报告。

- 周报：Markdown 格式，按 workspace × 主题分组，列关键成果和下周计划
- 月报：Swiss 风格单页 HTML，含 KPI 卡片、主题摘要、重点项目、里程碑时间线
- 历史管理：生成的报告持久化到本地，支持查看、下载、删除
- 报告质量遵循写在前面的指导原则，确保负面结果也不遗漏

报告 Prompt 已对齐 Comet Skill 的 `weekly-report` 与 `writing-monthly-reports` 能力，确保输出格式一致。

### 5.10 设置

配置：

- LLM Provider 管理——添加、选择、删除 Provider
- Model / API Base / API Key / Temperature / Max Tokens / Thinking 模式
- Workspace 管理——添加、编辑 workspace
- Git 知识镜像同步——配置远端仓库地址，同步 wiki 数据

---

## 六、MCP Server（面向 AI Agent）

Comet-Panel 内嵌 MCP（Model Context Protocol）Streamable HTTP 端点，让 AI Agent 可以直接查询知识图谱。

### 端点地址

```
POST http://localhost:8989/mcp
```

### 暴露的工具

| 工具 | 说明 |
|------|------|
| wiki_search | 语义搜索工程文档。输入查询关键词，返回最相关的文档列表 |
| wiki_component | 查看某个组件的详细信息，包括类型、引用关系 |
| wiki_neighbors | 查看某个组件在图谱中的 2-hop 邻居 |
| wiki_overview | 获取某个主题社区的 AI 生成综述 |
| wiki_read | 读取指定路径文档的原始内容 |
| wiki_lint | 检查文档健康度：列出死链、孤儿节点、缺失验证报告等 |

### Agent 配置示例

在 OpenCode（或支持的 Agent 平台）的 MCP 配置中添加：

```json
{
  "comet-wiki": {
    "url": "http://localhost:8989/mcp"
  }
}
```

配置后，Agent 可以执行：
- 搜索你项目中的设计文档
- 查看某个 Change 的详细信息和引用关系
- 阅读特定文档内容并进行讨论
- 检查文档健康度

这是 Comet-Panel 区别于纯查看面板的关键能力：**知识图谱不仅对开发者可见，对 AI Agent 也可编程访问**。

---

## 七、索引更新机制

Comet-Panel 通过 fsnotify 监听文件系统变更，自动增量更新知识索引。

流程：

1. 文件变化（.md / .comet.yaml）
2. fsnotify 检测并收集变更
3. 5 秒防抖窗口
4. 增量更新：仅处理变更文档
5. 重新分类 → 重新 embedding → 重新提取链接
6. 图结构原地更新
7. 前端收到 SSE 通知自动刷新

性能：

- 单文件增量更新约 232ms
- 全量重建约数秒（首次启动或手动触发）
- 搜索 <300ms

WebSocket 驱动的实时推送确保页面始终保持最新。

---

## 八、常见问题

### 8.1 无法搜索到刚写入的文档？

这是正常的。Comet-Panel 使用 5 秒防抖窗口 + 增量更新机制，保存后需要等待防抖窗口结束。页面顶部会出现"索引更新中"的蓝色提示条，提示消失后即可搜索到。

### 8.2 为什么某些目录没有被扫描？

以下目录默认跳过：
- `.git`、`node_modules`、`rootfs`
- 所有以 `.` 开头的隐藏目录
- `orin_bsp`、`qcom_bsp`、`argos-sdk`、`x5_sdk`、`mondo-ai` 等大型 SDK/BSP 目录

如果你需要特别扫描某个目录，可以添加 `wiki: true` 到文档的 frontmatter 中。

### 8.3 知识图谱中的节点和边是什么？

Comet-Panel 从你的工程目录中自动提取：

- **节点**：10 种组件类型（change / proposal / design / tasks / spec / plan / artifact / diagram / report / knowledge）
- **边**：4 层链接提取
  - YAML：`.comet.yaml` 的高置信引用
  - Markdown：文档内的 `[text](path)` 链接
  - 产物约定：同 Change 内 proposal→design→tasks→specs 自动连线
  - 向量：Ternlight embedding cosine 相似度 TOP3

### 8.4 如何重启服务？

```bash
# Systemd 服务
systemctl --user restart stele

# 直接运行
kill <PID>
./stele --port 8989 --dir /path/to/openspec
```

### 8.5 如何删除已验证测试通过的分支？

当前本地 GitButler 分支较多时，可以通过 `but` 管理：

```bash
but branch list          # 查看所有分支
but branch delete <id>   # 删除分支
but pull                 # 同步
```

---

## 九、架构概览

```
                   Frontend (React + Vite + Tailwind)
     WikiGraph · SemanticSearch · ChangeExplorer · ReportView · Chat
                          │ HTTP API
                   Go Backend (single binary)
     ┌─────────────────────────────────────────────────────────┐
     │  Wiki Engine                                            │
     │  scan → links (4 layers) → graph → embed → similarity   │
     │  → Louvain → community labels                           │
     │                                                         │
     │  Chat/LLM · Report · MCP Server                         │
     │  fsnotify watcher → incremental rebuild → SSE push       │
     └─────────────────────────────────────────────────────────┘
                          │
                    Ternlight (Bun)
                    384-dim embedding
```

Workspace 目录结构（数据源）：

```
/path/to/project/
├── openspec/
│   ├── changes/
│   │   ├── active-change/
│   │   │   ├── .comet.yaml
│   │   │   ├── proposal.md
│   │   │   ├── design.md
│   │   │   └── tasks.md
│   │   └── archive/
│   └── specs/
├── docs/
│   ├── superpowers/
│   │   ├── specs/
│   │   ├── plans/
│   │   └── reports/
│   └── reports/
├── knowledge/           ← Agent 产出的文档放这里
├── design_docs/         ← 设计文档
├── nv_docs/             ← 平台文档
└── ...
```

---

## 十、社区与源码

- 源码仓库：github.com/sudashannon/stele
- 技术栈：Go 1.26 + React 18 + Vite + Tailwind CSS
- Embedding：Ternlight（384 维 MiniLM）
- 协议：MIT
