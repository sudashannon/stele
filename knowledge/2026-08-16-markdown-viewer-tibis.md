---
title: Markdown viewer 扩展：Tibis 复用边界与 Stele 方案
tags: [stele, markdown-viewer, tibis, architecture, frontend]
---

# 目标

在不破坏 Stele 现有只读 artifact viewer、报告渲染和 workspace 路径鉴权的前提下，吸收 Tibis 的 Markdown 交互设计，先实施阅读器增强，再为未来独立编辑器保留接口边界。

# 已确认事实

## Stele 当前实现

- `web/src/components/MarkdownViewer.tsx` 使用 React 18、`react-markdown`、`remark-gfm`、`rehype-slug`。
- 当前已有：Front Matter 去除、h1-h3 目录、IntersectionObserver 当前章节、Mermaid/PlantUML、图片 lightbox、摘要缓存/生成、artifact tabs、收藏、分享、待办、变更跳转、相关会话和文档上下文 rail。
- `ReportView.tsx` 以 `path={null}` + `body={result.body}` 复用 Viewer 渲染报告；报告必须保持只读。
- `/api/artifact` 是 method-agnostic 的只读 handler：当前没有方法分派，也没有保存 API；服务端执行 workspace 解析、artifact 根目录授权和文件读取。未来增加 PUT 前必须先让非 GET 返回 405，再接入独立保存 handler。
- `web/package.json` 没有编辑器框架；已直接依赖 Markdown 渲染和 Mermaid。

## Tibis 当前 main 分支

来源：

- README：https://github.com/xbinator/tibis
- 入口：https://raw.githubusercontent.com/xbinator/tibis/main/src/components/BEditor/index.vue
- Markdown 布局：https://raw.githubusercontent.com/xbinator/tibis/main/src/components/BEditor/Markdown.vue
- 大文档加载：https://raw.githubusercontent.com/xbinator/tibis/main/src/components/BEditor/hooks/useRichEditorLoad.ts
- Markdown 解析：https://raw.githubusercontent.com/xbinator/tibis/main/src/components/BEditor/utils/richMarkdownParser.ts
- 扩展集合：https://raw.githubusercontent.com/xbinator/tibis/main/src/components/BEditor/hooks/useExtensions.ts

Tibis 的有效设计：统一 Editor Controller、Rich/Source 双 pane、目录/锚点、统一搜索和选区、Front Matter 节点、大文档 loading/retry/source fallback、解析缓存和源码行映射。

Tibis 不是 Stele 的可直接替换组件：它是 Vue 3 + Electron + TipTap 3 的本地编辑器，Stele 是 React + Go 的只读知识库 Web UI。GitHub API 返回 `license: null`，`/LICENSE` 也不存在，尽管 README 声称 MIT。因此本项目只复用架构思想、公开依赖和兼容的交互模式，不复制 Tibis 源码，除非许可证状态得到单独确认。

# 复用决策

| Tibis 设计 | Stele 决策 |
|---|---|
| 统一 Editor Controller | 先建立只读 `MarkdownDocumentModel` 和 Viewer controller，不引入编辑器依赖 |
| Rich/Source 双视图 | 第一阶段只做只读 Source View；原始 Markdown 是工程文档保真基准 |
| Front Matter 节点 | 做可折叠元数据卡；默认折叠时不挂载字段文本，展开后才显示，保持旧正文查询契约 |
| 30k 字异步解析和分帧挂载 | 先引入 loading/error/retry 和块级结构；只有实测卡顿才做分段/虚拟化 |
| 选区 AI/comment toolbar | 第一阶段复制/引用/行号；AI 和评论需要新的 API/持久化契约 |
| Tiptap 富文本编辑 | 延后到独立 `MarkdownEditor`，不能污染 `MarkdownViewer` 或报告渲染 |

# 审查修订

- 搜索 Escape 层级固定为：搜索 → 图片 lightbox → Viewer；搜索打开时必须先关闭搜索，不触发 `onClose`。
- 标题 ID 兼容测试必须覆盖重复标题、h4/h5 与 TOC 标题冲突、最多 3 个前导空格的 ATX 标题、带行内 Markdown 的标题；模型必须按 h1-h6 的完整顺序分配 slug，再只展示 h1-h3。
- 现有 Front Matter 测试保留“正文中不存在字段文本”的契约；元数据卡采用 lazy mount，展开时单独验证字段可见。
# 实施范围

## 当前阶段

1. 统一 Markdown 文档模型：raw、frontmatter、body、headings、source line ranges。
2. Front Matter 可折叠展示。
3. 正文块增加 source-line 锚点，目录跳转和当前标题继续工作。
4. 文档内搜索、只读源码视图、代码块复制和阅读工具栏。
5. 保持 `path=null` 的报告 body 分支完全只读。

## 后续阶段

- 选区摘要和引用待办。
- KaTeX/脚注/更完整的代码高亮。
- 大文档性能实验和分段渲染。
- 有版本校验、原子写入和 workspace 鉴权后，建立独立 MarkdownEditor。
- 富文本模式必须通过 Stele 文档样本的 Markdown round-trip 测试后再决定。

# 风险与守卫

- 不添加 `rehype-raw`，除非同时配置严格 `rehype-sanitize`。
- 不给 Viewer 增加保存能力；未来保存必须复用 `resolveWorkspaceConfig` 和 `artifactPathAllowed`，并使用 ETag/If-Match 防止覆盖并发修改。
- 不改变 artifact 相对链接的 workspace 转发和后端授权。
- 不把 frontmatter 重新拼进正文引用；报告证据仍只认文档正文和 D<n> 机制。
- 不让 Tiptap 序列化覆盖原始 Markdown，直到自定义 frontmatter、Mermaid、PlantUML、任务列表和异常 Markdown 都有 round-trip 证据。

# 阶段验收

- Viewer 原有测试全部通过。
- 新增测试覆盖：frontmatter 展示/折叠、源码切换、搜索匹配、代码复制、source-line 锚点、报告 body 不出现编辑动作。
- 浏览器实测普通文档、长文档、报告、Mermaid/PlantUML、图片、artifact 相对链接和窄窗口布局。
