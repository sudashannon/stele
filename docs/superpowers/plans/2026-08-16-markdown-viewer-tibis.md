# Markdown Viewer 扩展实施计划

## 目标

参考 Tibis 的统一 Markdown 编辑器交互，但在 Stele 中保持 `MarkdownViewer` 的只读职责：先增强工程文档阅读体验，再为未来独立编辑器提供清晰边界。

## 现状与约束

- Viewer：`web/src/components/MarkdownViewer.tsx`
- 测试：`web/src/components/MarkdownViewer.test.tsx`
- artifact API：`main.go:handleGetArtifact` 是 method-agnostic 的只读 handler，当前没有方法分派和保存 API；未来接入 PUT 前先让非 GET 返回 405，再增加独立保存 handler。
- 报告复用：`web/src/components/ReportView.tsx` 使用 `path=null`、`body` 渲染
- 当前技术栈：React 18 + react-markdown + remark-gfm + rehype-slug + Mermaid
- 不复制 Tibis 源码：Tibis GitHub API 的 license 字段为空，仓库没有 LICENSE；只复用结构和依赖理念。

## Reviewer gate

计划审查结论：GO。实施前纳入四项修订：

1. Front Matter card collapsed 时 lazy mount，不把字段文本放进 DOM；展开时再显示，保持现有“正文不包含 frontmatter 字段”的测试契约。
2. Escape 层级固定为“搜索 → 图片 lightbox → Viewer”，搜索关闭时不得触发 `onClose`。
3. 标题 slug 测试覆盖重复标题、h4/h5 冲突、最多三个前导空格和行内 Markdown；slug 分配按 h1-h6 的完整序列执行。
4. `/api/artifact` 现状描述为 method-agnostic read-only；未来 PUT 前先增加非 GET 的 405 方法分派。

## 设计原则

1. 源码保真优先：工程 Markdown 的 Front Matter、Mermaid/PlantUML、自定义字段和行号不可因富文本序列化丢失。
2. 阅读器和编辑器分离：`MarkdownViewer` 不增加 save/onChange；未来另建 `MarkdownEditor`。
3. 一次解析，多种消费：TOC、标题、source-line、Front Matter 和搜索共享文档模型。
4. 报告安全：`path=null` 的 body 只允许本地阅读动作，不显示 artifact、保存或文档写操作。
5. 不扩大 HTML 攻击面：不启用 raw HTML，除非同时做严格 sanitize。
6. 先证据后优化：只有真实文档性能数据证明必要时，才引入分段渲染或编辑器级缓存。

## 阶段一：文档模型与元数据

### 变更

- 新增 `web/src/components/markdownDocument.ts`：解析 raw Markdown 的 Front Matter、body、标题和 source line ranges。
- 修改 `MarkdownViewer.tsx`：使用文档模型生成标题、目录、标题展示和 Front Matter metadata card。
- Front Matter 默认折叠；解析失败时保留 raw 内容。
- 给渲染块添加 `data-source-start` / `data-source-end`，不改变链接授权。

### 验收

- Front Matter 不再静默消失，但正文语义不变。
- fenced code 内的 `#` 不产生目录标题。
- 标题 ID 与当前 `rehype-slug` 行为兼容。
- 报告 body 不显示路径型元数据和编辑动作。

## 阶段二：阅读交互

### 变更

- 新增 Viewer 内搜索栏，支持匹配计数、上一个/下一个和 Escape 关闭。
- 新增只读 Source View：原始 Markdown、行号、复制全文；先不引入编辑器框架。
- 阅读工具栏加入目录切换、源码切换、复制标题链接。
- 代码块加入复制按钮和语言标签；保留 Mermaid/PlantUML 专用渲染及错误降级。
- 窄窗口使用抽屉/按钮，不依赖 1200px 右侧 rail。

### 验收

- 搜索不会改变 artifact path 或发起写请求。
- 源码视图与服务端返回内容一致。
- 报告 body 可以正常切换阅读/源码，不出现保存按钮。
- 代码复制成功/失败都有可见反馈。

## 阶段三：性能与工程交互

仅在阶段二浏览器实测发现瓶颈后实施：

- 大文档 loading/error/retry 状态机。
- AST/分段懒渲染或虚拟化。
- 选区复制、引用待办、选区摘要。
- KaTeX、脚注和动态代码高亮。

## 阶段四：独立编辑器（后续，不在本轮默认实现）

前置条件：

- workspace-authorized PUT API；
- ETag/If-Match 并发控制；
- 原子写入和失败恢复；
- Front Matter、Mermaid、PlantUML、任务列表、异常 Markdown 的 round-trip 语料测试；
- 明确 CodeMirror source-first 与 TipTap rich mode 的取舍。

组件边界：

```text
MarkdownViewer   -> 只读知识库/报告阅读
MarkdownEditor   -> 可编辑 artifact
ReportView       -> 永远只读
```

## 验证计划

- `npm run test -- MarkdownViewer`：新增契约测试。
- `npm run test`：前端完整测试。
- `npx tsc -b`：类型检查。
- `npm run build`：生产构建。
- 浏览器验证：普通文档、Front Matter、长文档、报告、Mermaid/PlantUML、图片、相对链接、窄窗口、源码视图、搜索和复制。
- 审查重点：未引入写 API、未改变 artifact 授权、报告路径为空时无编辑动作、未启用未经清理的 raw HTML。
