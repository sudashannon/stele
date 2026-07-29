---
title: Comet Panel Todo + MCP + Agenda UI 实施记录
tags: [comet-panel, todo, mcp, ui, deepwork]
---

# 目标与确认

用户已在三个可交互 Demo 中确认采用方案 C：日程分组 + 今日焦点。实现范围包括左侧 Todo 主视图、REST/MCP 增删改查、Change/Wiki 关联、SSE 实时同步和上下文创建入口。当前阶段不做 Git 写操作。

# 已确认的产品边界

- Todo 是单 OS 用户的高频工作覆盖层，持久化在 `~/.comet-panel/todos.json`；不是 Wiki Component，也不是 OpenSpec `tasks.md` 的镜像。
- Todo 完成/删除不修改 `tasks.md`、`.comet.yaml`、Change phase 或任何 Wiki 文档。
- 一个 Todo 最多关联一个 Change（持久键 `workspace + change name`），可关联多个 Wiki 文档（`Component.ID + titleSnapshot`）。所有 Change 关联查询统一经 `changeRefKey(workspace, name)` 归一化；从 Change 关联 Wiki 节点时只由该业务键解析当前 `.comet.yaml` Component.ID，不并存第二套持久键。
- Wiki 标题在 GET 时实时解析；snapshot 只在文档失联时兜底。
- 采用三态 `open | in_progress | done`，四级优先级 `urgent | high | normal | low`。
- 方案 C 是默认且唯一 V1 视图：逾期/今天/明天/稍后/无日期分组，右侧焦点详情；不实现看板切换。
- V1 不做子任务、重复规则、通知提醒、拖拽排序、批量删除或 Todo 图谱节点。

# 已确认的源码事实

- App 级主视图和左侧导航：`web/src/App.tsx`、`web/src/components/SideRail.tsx`。
- Change 稳定业务键和 Wiki component ID：`scanner.go`、`wiki/wiki.go`、`web/src/api/types.ts`。
- MCP 入口、工具注册和 JSON-RPC：`wiki/mcp.go`；现有六个 `wiki_*` 工具保持兼容。
- 命名 SSE 事件：`wiki/sse.go`、`web/src/hooks/useWikiEvents.ts`。
- 轻量本地状态惯例：`bookmark.go`；Todo 会在此基础上增加 schema envelope、0600 和原子 rename。
- Windows `http://localhost` 到 WSL 的实际 RemoteAddr 已探测为 `127.0.0.1`，因此 loopback 写保护兼容当前 Windows 本机浏览器。
- 当前 8989 已转发到 LocalSubnet；新增写工具必须显式保护。

# 共享实现契约

## Go 领域模型与存储

新增 `internal/todo`：

- `Todo`, `ChangeRef`, `WikiRef`, `Status`, `Priority`。
- `Store` 是 REST 和 MCP 唯一共享实例。
- 文件 envelope：`schemaVersion`, `revision`, `items`。
- `List/Create/Update/Delete` 在进程 mutex 下执行；写入临时文件后 rename；成功 mutation 解锁后调用 `onChange(revision)`。
- ID 使用 crypto/rand；时间统一 UTC RFC3339 JSON；status=done 设置 completedAt，重新打开清空。

## REST

- `GET /api/todos`：query 支持 status/workspace/change/wikiComponentId/q，返回 `{items, counts, revision, writable}`。
- `POST /api/todos`。
- `PATCH /api/todos/{id}`。
- `DELETE /api/todos/{id}`，禁止 bulk delete。
- 所有 mutation 经过同一 WriteGuard：RemoteAddr 必须 loopback；有 Origin 时必须 same-origin。非本地访问 GET 仍可读并返回 `writable:false`。

## MCP

新增 `todo_list`, `todo_create`, `todo_update`, `todo_delete`。

- `todo_list` 只读；其余 mutation 必须 loopback 且 Bearer token 正确。
- token 自动生成于 `~/.comet-panel/mcp-write-token`，权限 0600。
- `wiki.API` 注入共享 `internal/todo.Store` 和 token，不复制状态，不改变现有 `wiki_*` 工具行为。
- 工具返回 JSON text content；无 bulk delete。

## SSE

Store mutation 广播：

- event: `todos-updated`
- data: `{"revision": N}`

前端扩展 `useWikiEvents` options：新增 `onTodosUpdated?: (revision: number) => void`，显式注册 `todos-updated` listener，解析 revision 后 refetch GET `/api/todos`；不传 diff，不触发 Wiki rebuild。

## 前端

- 新增 `todos` App view 和 `TodoPanel`（方案 C）。
- SideRail 在 Change 后加入 Todo 图标，badge 显示 open+in_progress，0 隐藏，99+ 封顶。
- 命令面板加入“打开待办”“新建待办”；快捷键 `Ctrl+8`，不调整现有 1~7。
- 主视图：顶部日完成度；左侧视图/Workspace/关联筛选；中间按日期分组；右侧焦点详情、Change/Wiki 关联和状态操作；窄屏折叠筛选与详情。
- `ChangeDetail` 提供“+ 待办 / 待办 N”，预填 workspace+change。
- `MarkdownViewer` 提供“+ 待办”，预填当前 Wiki ref，并仅建议路径推导的 Change。
- Todo chips 复用现有 Change/Markdown 导航。
- `writable:false` 显示局域网只读横幅并禁用 mutation 控件。

# 分阶段执行计划

1. 后端能力
   - 建立 `internal/todo` 模型、原子 Store 与单元测试。
   - 在 root 接 REST、WriteGuard、token 与 SSE callback。
   - 在 `wiki/mcp.go` 接四个 Todo 工具和鉴权测试。
   - 阶段验证后独立 reviewer 审查。
2. 前端体验
   - 建立 types/client/useTodos 数据流与方案 C TodoPanel。
   - 接入 SideRail、App、快捷键、SSE、ChangeDetail、MarkdownViewer。
   - 由 designer 保持已确认 Demo C 的布局层级、节奏和交互，再由 task worker 机械接线和测试。
   - 浏览器实际验证后独立 reviewer 审查。
3. 验收部署
   - 运行定向与全量前后端回归；记录既有无关失败。
   - OCR 优先进行代码审查，必要时 reviewer fallback。
   - 构建嵌入式前端和 Go 二进制，替换生产服务。
   - 从 REST、MCP、SSE、Windows localhost UI 和 LAN read-only 路径端到端冒烟。

# 状态

- 设计确认：完成（方案 C）。
- 实施计划评审：GO。首轮指出 `todos-updated` 前端监听未写入明确契约，并建议统一 Change 关系 reducer；两项均已修订，复审通过。
- 后端：完成，独立 reviewer 判定 GO。`internal/todo`、root REST 和 `wiki` MCP 定向测试通过；真实进程验证完整模型创建、RFC3339→UTC、PATCH 关系清空、筛选/计数、MCP 十工具、Bearer 拒绝/授权写入及 `todos-updated` revision。冒烟修正 workspace/clear 语义后，又落实 reviewer 的 callback/API 配置并发快照、大小写搜索、MCP 实时 Wiki 标题、schema/type 校验、更新关系校验和默认路径统一；复测全部通过。
- 前端：完成，独立 reviewer 判定 GO。方案 C 保持日程分组、左筛选、中列表、右焦点层级；接入单一 Todo 数据流/SSE、SideRail 徽标、Ctrl+8、命令面板、Change/Markdown 上下文、关联编辑和 LAN 只读态。Reviewer 的稳定 SSE handler、DST 安全明日计算和 Change workspace fallback 已落实；生产构建与全量 27 files / 273 tests 复测通过（仅既有 WorkspaceChips act 警告）。Windows 默认 Edge 实际页面由用户截图确认 1452×760 下分组、徽标、workspace selector 与关联 chips 正常。
- 验收部署:完成。`go vet ./...`、排除既有 `TestShareManager_Revoke` 死锁后的全部 9 个 Go package、前端生产构建及 27 files / 273 tests 通过;未排除的 `go test ./...` 可复现该既有 share 测试死锁。OCR 因自身 line-range / missing-file 错误超时后,独立最终 reviewer 对 REST/MCP/SSE/Store/UI 跨边界审查判定 GO。生产服务已重新构建并部署至 `localhost:8989`,systemd user service 为 active;真实生产进程完成 REST 创建 → MCP 查询/授权更新与关系清空 → MCP 单项删除,验证 10 tools、UTC 规范化、0600 token/todos 文件和清理后零项;LAN `10.0.28.45:8989` GET 返回 `writable:false`,POST 返回 403。最终生产页面已在 Windows 默认 Edge 打开。
