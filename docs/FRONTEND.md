# FRONTEND —— 前端接手说明

[AGENTS.md](../AGENTS.md) 的配套文档：改前端前先完整读本文件与真实代码。本文与 AGENTS.md 冲突时，以 AGENTS.md 为准。

技术栈：React 19、TypeScript 5.9、Vite 7、Tailwind CSS 4（`@tailwindcss/vite`）、lucide-react。没有路由库与状态库：hash 路由在 `App.tsx`，全局状态在 `lib/context.ts` 的 `AppContext`。

| 位置 | 内容 |
| --- | --- |
| `src/App.tsx` | 布局、导航分组（知识库（导入（上传文件 + 扫描文件夹两个 Tab）、资料、要点、问答） / 公众号 / 设置（向量化、提示词、安全））、平台加载、健康状态、语言切换；默认导出的 `App` 是锁屏闸门（`/auth/status` → 锁屏/工作区），工作区是 `Workspace` |
| `src/lib/i18n.ts` | 中英字典与 `t()`；默认中文（`localStorage` 无偏好时），键 `clovergo.lang`；`en` 定义键类型，`zh` 必须键键对齐 |
| `src/lib/api.ts` | `api`、`postJSON`、`putJSON`、`deleteJSON`、`withQuery`、`ApiError`、`describeError`（错误码 → `error.<CODE>` 双语提示）、解锁令牌（`getToken/setToken`，键 `clovergo.token`，自动带 `X-CloverGo-Token`，401 `AUTH_REQUIRED` 时清令牌并派发 `clovergo:locked`）、`downloadFile`（带令牌下载 ZIP） |
| `src/lib/types.ts` | 与后端 JSON 对应的类型；后端字段变化先改这里 |
| `src/lib/nav.ts` | 平台 ↔ 视图的映射（`PLATFORM_NAV`：配置/创作/列表/详情）；导航分组由它生成，平台删掉即从菜单消失；详情页不在菜单里，靠 `draft:<id>` 进入 |
| `src/lib/context.ts` | 视图、平台、跨页跳转（`jumpTo(view, target)`，target 形如 `knowledge:12` / `draft:3` / `document:5`（资料详情，视图 `documentDetail`） / `platform:gongzhonghao`）、toast |
| `src/lib/format.ts`、`hooks.ts` | 标签映射、日期数字格式、`useAsync`、计时 |
| `src/components/` | `ui.tsx` 基础组件；`knowledge.tsx` 知识卡与 Evidence；`draftContent.tsx` 文章渲染器；`Markdown.tsx` 安全的 Markdown 渲染；`LockScreen.tsx` 锁屏 |
| `src/views/` | `MaterialsUpload`（仅上传文件：选/拖入后立即逐个上传；界面已无粘贴与主题输入，后端 `/materials/text` 保留）、`MaterialRoots`（扫描文件夹：选择文件夹 → 自动登记并扫描 → 勾选导入；与上传同在「导入」页的两个 Tab）、`MaterialsList`（普通资料列表：每行 详情/提炼/删除）、`DocumentDetail`（资料详情，路由 `#/documentDetail?document=<id>`，仅显示摘要与原文，返回列表）、`Knowledge`（界面称「要点」）、`Agent`（问答：左侧会话列表 + 右侧对话，会话存 `localStorage` 键 `clovergo.chats`，删除会话只删本地记录）、`Styles`（配置：风格设置 / 接口配置 / 格式规则三个 Tab）、`DraftCompose`（创作）、`Drafts`（列表）、`DraftDetail`（草稿详情：查看 / 修改 / 删除 / 推送；上传位在图片旁——长文封面提示词旁一个封面位，图文帖每张图旁一个位、第 1 张即封面，上传文件只在前端内存里，推送时随表单发送）、`Vectors`（向量化）、`Prompts`（提示词层微调与最终 Prompt 预览）、`Security`（设置/修改/移除全局密码） |

开发方式：

```powershell
Set-Location .\clovergo           # 从项目父目录进入
go run .                        # 后端 127.0.0.1:8088（需要 .env）
Set-Location .\frontend
npm ci
npm run dev                     # 127.0.0.1:5173，/api 与 /health 代理到 8088
npm run build                   # tsc --noEmit + 输出到 ..\web\dist
```

前端必须遵守：

- **构建产物要提交**：`web/dist` 由 `main.go` 的 `//go:embed all:web/dist` 内嵌，改前端后必须 `npm run build` 并提交 `web/dist`；客户机器不装 Node。
- **静态资源本地加载**：不引用 CDN、外部字体、外部图片或统计脚本；favicon 用内联 data URI。AI 功能仍调用方舟云端。
- **不注入 HTML**：模型输出与资料文本一律按文本渲染；不使用 `dangerouslySetInnerHTML`、`innerHTML`；Markdown 只用 `components/Markdown.tsx` 这类安全渲染。
- **请求约定**：写请求用 `postJSON`/`putJSON`（自动带 `application/json`）；保存平台风格必须带 `if_revision`，处理 409 `REVISION_CONFLICT`。
- **长任务**：导入总结是同步长请求；提炼与草稿生成入后台队列，前端轮询状态。都要有进行中状态、可见耗时；界面不显示、不提供查看 Run ID 的入口（后端 Run 记录与 `/runs` 接口保留）；多文件按顺序逐个调用，单个失败不影响其余。
- **查看原文与向量化**：资料详情页用 `GET /documents/:id/text` 读回原文件规范化后的整篇文本，按纯文本渲染（不注入 HTML）；向量化页用 `GET /stats`（含 `document_vectors`、`document_vectors_total`）与 `GET /health/queue`（含 `document_vector_pending`）展示覆盖率，`POST /vectors/reindex` 同时补齐知识与资料。`stats.pending` 是「已归档未提炼」的静止数量（不是后台任务），顶栏的「提炼中」徽标与 4 秒轮询只按 `stats.processing` 判断。
- **文案真实**：公众号文章只能称为“文章/草稿”，配图只能称为“配图提示词”；推送只能称为“推送到草稿箱”或（所有者显式选择时）“推送并发布/已提交发布”；不得出现“一键发布”“自动发布”“自动生成图片”等未实现能力；发布必须有二次确认。
- **双语**：界面文案一律走 `t()`，默认中文、英文同步维护；后端返回的数据与模型输出不翻译。
- **通用领域**：不得写死某个领域的文案、示例或平台 ID；知识类型从 `GET /kinds` 读取（内置分类 + 在用自定义分类），不硬编码在界面里。
- **数字只写一处**：格式相关的数字（标题 30、每章 800、全文 2000、章节 2–5、标签 5……）只存在于 `internal/platforms` 的常量里。提示词、校验器、配置页都读同一份——配置页用接口返回的 `rules` 渲染，**前端不得另抄一份数字**。
- **提示词层**：设置页可整层替换 `internal/prompts/layers/*.md` 的文本（`PUT /api/v1/prompts/layers/:key`，带 `if_revision`），`POST .../reset` 恢复内置；分类层与格式数字层不可编辑。界面不得自行拼接提示词文本，最终 Prompt 一律以 `GET /api/v1/prompts` 为准。
- **可访问与适配**：浅色/深色（跟随系统）、窄屏可用（≥360px 无横向滚动）、按钮与表单有可读标签。
- 本地偏好可存 `localStorage`，读写包在 try/catch 里，失败不影响功能。

建议的前端验证：`npm run build` 通过；再走一遍导入、知识库、草稿生成、草稿详情编辑与删除、推送配置各页并检查控制台无报错与截图。
