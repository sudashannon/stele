# Markdown 编辑功能实施计划

## 目标

在 Stele 中加入安全的 Markdown 编辑能力：保留原始 Markdown，支持保存已有工程文档，并在并发修改时拒绝覆盖。`MarkdownViewer` 继续只读；编辑能力由独立 `MarkdownEditor` 承担。

## 已确认边界

- artifact 读取入口是 `main.go:handleGetArtifact`，授权入口是 `resolveWorkspaceConfig` + `artifactPathAllowed`。
- 当前 `/api/artifact` 只有 GET；前端只有 `fetchArtifactContent`。
- 当前没有 CodeMirror、TipTap 或其他编辑框架依赖。
- 报告通过 `MarkdownViewer path=null body=...` 渲染，不能出现编辑、保存或写请求。
- 现有阅读器已经处理 Front Matter、Mermaid、PlantUML、任务列表和异常 Markdown；编辑器不能通过富文本序列化破坏它们。

## 设计决定

### 编辑器采用 source-first textarea

本轮不引入 TipTap。受控 textarea 可以原样保留 Front Matter、Markdown、Mermaid/PlantUML 和未知语法，依赖为零，且后续可以把编辑表面替换为 CodeMirror 而不改变保存协议。

### 保存协议

`PUT /api/artifact?path=<path>&workspace=<alias>`：

- 仅允许已存在且通过 `artifactPathAllowed` 的文本/Markdown 文件；本轮不创建、不重命名、不移动、不删除、不编辑二进制文件。另对最终 `EvalSymlinks` 路径按所有 workspace 类型重新检查 resolved root containment。
- 要求强的、显式的 `If-Match`；缺失或 `*` 返回 `428 Precondition Required`，弱 ETag 和格式错误返回 `400`。
- handler 在 artifact 写锁内读取当前文件字节并计算强 ETag；不使用 mtime 或客户端缓存的 hash。当前内容 ETag 不匹配返回 `412 Precondition Failed`，响应带当前 ETag；客户端保留用户未保存文本。
- 成功返回 `200`、新 ETag、路径和字节数。
- GET 成功响应也带 ETag，保持现有读取行为兼容；HEAD 继续走 GET 分支。
- 非 GET/HEAD/PUT 方法返回 `405` 和 `Allow: GET, HEAD, PUT`。
- 请求体限制在服务端固定上限；拒绝超限，不截断。

### 写入安全

- 使用同目录临时文件、继承原文件 mode、写入后 fsync、rename 替换 resolved target，再尽可能 fsync 父目录。
- 保存前后使用同一进程内的 artifact 写锁，避免两个请求同时通过同一个 ETag 检查。
- 失败时原文件必须保持不变；文件级 symlink 不能被替换成普通文件。
- 不允许通过 workspace/path 参数绕过现有根目录和 symlink 守卫。

### 前端边界

- 新增 `MarkdownEditor.tsx`：加载内容和 ETag、dirty 状态、保存中/成功/失败、冲突保留、重新加载。
- `MarkdownViewer` 增加可选 `onEdit`；只有真实 artifact viewer 传入。
- `App.tsx` 在当前 viewer overlay 中切换 `MarkdownViewer` / `MarkdownEditor`，不让报告 body 和 session transcript进入编辑器。
- 保存成功后更新本地 ETag，保持编辑器打开；关闭 dirty 编辑器前要求确认。
- 冲突状态提供“保留我的修改”和“重新加载服务器版本”，不自动覆盖。

## 阶段与验收

### 阶段 1：后端契约

- [ ] `/api/artifact` 方法分派和 ETag
- [ ] 授权 PUT、大小限制、原子写和并发锁
- [ ] GET/HEAD/PUT/405/428/412/415/400/403/404 契约测试
- [ ] 非 Superpowers symlink 越界、`If-Match: *`、同一旧 ETag 并发 PUT（一成功一 412）、写失败保持原文和 mode

### 阶段 2：编辑器

- [ ] API client/types
- [ ] 独立 source-first editor
- [ ] App viewer/editor 切换
- [ ] dirty、保存成功、失败、冲突和报告只读测试

### 阶段 3：验证

- [ ] reviewer 独立审查阶段结果
- [ ] Go 全套测试
- [ ] 前端全套测试、类型检查、生产构建
- [ ] 浏览器验证打开、编辑、保存、刷新和并发冲突
- [ ] 部署后确认服务和静态 bundle

## 非目标

- 富文本编辑器或 Markdown round-trip serializer
- 新建/重命名/移动/删除文档
- 报告 HTML/Markdown 编辑
- session transcript 编辑
- 选区 AI 操作、撤销重做、语法高亮、虚拟化；这些属于后续阶段
