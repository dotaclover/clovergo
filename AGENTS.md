# CloverGo 开发说明（人与 AI 共用）

本文件是 CloverGo 的**核心约束文档**，只保留约束与事实骨架；细节分文档：[docs/BACKEND.md](docs/BACKEND.md)（后端结构与关键行为）、[docs/FRONTEND.md](docs/FRONTEND.md)（前端接手说明）。用户视角的能力与 API 见 [README.md](README.md)（中文，默认）与 [README.en.md](README.en.md)。**改哪一层，先完整读对应分文档**，再读与任务相关的真实代码；分文档与本文件冲突时，以本文件为准。任何结论以仓库实现、数据库和验证结果为准，不以计划或注释推断功能存在。

CloverGo 以 MIT 协议开源，是一个 AI 编程项目（TraeWork、Claude Opus、GPT 参与）。本文件同时约束人与 AI。

## 1. 产品事实（v0.2.0）

CloverGo 是 Go + SQLite + 内嵌 React 的本机**个人知识库 + Agent**。资料库是底座；Agent、带引用问答、公众号文章草稿是资料库驱动的服务。**一个实例只有一个知识库**，没有"空间/租户/Pack"维度：资料、知识、草稿都在一起检索。

```text
平台风格覆盖（SQLite，带修订） + 公众号接口配置（SQLite）
资料（MD/TXT/DOCX 批量 / 粘贴 / 扫描已登记文件夹） → 方舟总结整篇后**只归档，不自动提炼** → 所有者在资料列表显式「提炼」→ 整篇单次提炼，逐条校验连续原文 Evidence → 已确认知识（版本、使用次数、向量）
→ retrieve（关键词 FTS/LIKE + 豆包语义，RRF 融合）
→ Agent（agent.chat） / 带引用问答（knowledge.answer） / 公众号文章草稿（media.draft，可从知识或资料原文出发） → 推送到公众号草稿箱
→ Run、事件、token、Artifact、Prompt 版本、ZIP；发布由所有者在推送时显式选择
```

- 平台只有一个：`gongzhonghao`（公众号），两种格式都是代码：`article`（必须有引言和结尾的分章文章）与 `gallery`（图文帖「小绿书」，推送时 `article_type: newspic`）。字数、章节数、图片数等是 `internal/platforms` 里的常量，**所有者只能改风格规则**（`style_rules`），数字不可调。早期的小红书/头条/抖音/视频号平台已连同校验与渲染器移除，不要恢复。
- **没有示例范文（模板）功能**：写文章只依据创作灵感、来源与平台风格；`draft_templates` 已从 schema 删除，不要重新引入模板页、模板接口或模板提示词层。**没有选题工作流**：写什么由「创作灵感」或「创作来源（指定知识点、资料原文、随机知识点）至少一项」决定。
- **导入只归档，不自动提炼**：上传后只做一次方舟总结写 `documents.summary`；提炼是所有者在资料列表的显式操作（整篇单次调用，逐条校验 Evidence）。资料与知识是两张表，一条知识只有一份来源与一处证据，**故意不做跨资料合并**。
- **Agent** 是最多 8 轮模型调用的**只读**工具循环（工具与细节见 docs/BACKEND.md）；回复中的 `[K<id>]` 只能是本次工具返回过的 ID（违规重试一次，仍违规则整次失败，校验始终在代码里）；对话由客户端保存并每轮发回，服务端不保存会话。没有任意 Shell/URL/路径/写库工具；**新增工具必须只读，或先在本文件写明信任边界**。
- **推送只能到公众号草稿箱**，发布仅当所有者显式选择「推送并发布」；Agent 与任何自动流程不得触发发布。图片在推送时本地渲染（标题 SVG 卡片 → 本机光栅化），**不请求图片模型**。
- 知识有 `read_count`：展开原文证据时 `POST /knowledge/:id/read` 记一次，给人回看用，不参与任何校验。
- 扫描导入、微信推送流程、图片渲染、SOCKS5 代理、草稿引用删除等**关键行为细节见 [docs/BACKEND.md](docs/BACKEND.md)**。其中扫描导入的信任边界是硬约束：只处理 `.md/.markdown/.txt/.docx`、不跟随符号链接、导入 `path` 必须 `filepath.IsLocal` 相对路径、最多 5000 文件；Agent 不接受任意磁盘路径。
- 其余未实现能力见 README「明确未实现 / Not implemented」；任何代码、文档、UI 文案都不得把它们写成已支持。

## 2. 硬性约束

### 信任模型：代码核心 + 所有者档案 + 不可信数据

- **代码核心**：`internal/prompts/layers/*.md` 里的提示词文本（编译期 `//go:embed` 进二进制，`CoreVersion` 由所有层文件的内容哈希自动生成），以及 `internal/ark`、`internal/service` 中的校验。改层文件后重新编译即改变版本，不再手工加号。
- **所有者档案**：平台风格、公众号接口配置与**提示词层替换**（`prompt_overrides`），由本机实例所有者通过界面/API 编辑。平台风格带修订号与 `if_revision` 乐观锁；提示词层整层替换、同样带 `if_revision`，删除覆盖即恢复内置文本。这些内容会被拼进 Prompt 或用于推送，但都不能关闭代码校验。
- **不可信数据**：上传资料、粘贴文本、主题、角度、受众。只能作为 user 消息中的数据，不能改变 Prompt、平台设置、知识类型或工具。
- 以下校验永远在代码里执行，不得因为“Prompt 已经要求了”而删除：Evidence 是整篇资料规范化文本的连续子串且 rune 偏移可复算；**提炼输出的知识类型属于 `domain.Kinds`**（内置分类是提炼工具 schema 的 enum）；问答只引用本次上下文的 Knowledge ID，Agent 回复中的 `[K<id>]` 只能是本次 Run 工具返回过的 ID；标题、摘要、引言、结尾字数，章节数量与顺序，单章与（引言+章节+结尾）总字数，配图与图注成对，标签数量。草稿生成与人工编辑都走 `ark.ValidateArticle`。
- Prompt 只能由 `internal/prompts` 组装，`internal/ark` 只接收组装好的文本；每次调用记录 `Composed.PromptVersion()`。
- 自媒体输出：不编造经历/案例/数据/考试或销量等结果，不承诺效果，不引用热点，不声称已发布或已生成媒体（只有所有者选择「推送并发布」且微信返回 `publish_id` 后才可显示“已提交发布”）；UI 保留“配图由本机渲染、发布前请检查”的提示。

### 数据与检索

- 一个 Go 程序、一个 SQLite、一个资料目录；一个 SQLite 只允许一个进程。启动顺序：绑定端口 → 打开数据库与迁移 → 恢复残留工作（提炼中的资料退回 `ready`，累计次数达 `MaxDocumentAttempts` 则 `analysis_failed`；`running` 的 Run 标为 `RUN_INTERRUPTED`；生成中的草稿标为 `failed`）→ 服务。
- 迁移：`001_baseline.sql` 基线 + `002_knowledge_fingerprint_scope.sql`（知识指纹改为按 `(document_id, fingerprint)` 唯一，避免跨资料/新版本的同文知识被吞掉）+ `003_app_lock.sql`（全局密码 `app_lock` 表）+ `004_library_roots.sql`（`library_roots` 表，`documents` 加 `root_id/rel_path/missing/origin_size/origin_mtime`）+ `005_wechat_publish.sql`（`media_drafts` 加 `wechat_publish_id/wechat_published_at`）+ `006_document_sha_scope.sql`（去掉 `documents.sha256` 全局唯一，查重只与同一 `source_key` 的最新版本比较）。基线（重置时把原 001–015 合并为当前完整 schema，并去掉无代码读写的 `draft_templates`、`artifact_knowledge`；`knowledge_fts` 与触发器由 `initFTS` 在代码里创建）。没有 seeder，平台默认值、提示词层都是代码。合并前的数据库记录着已不存在的版本号，启动时以 `ErrIncompatibleSchema` 拒绝，需移走后重建。新迁移从 `007` 起编号，必须同时在 `repository/sqlite.go` 的 `migrations` 切片登记；已存在的 migration 不得修改，数据库变化新增下一编号。v1 数据库（含 `schema_migrations` 表）必须明确拒绝。
- 上传时 `extractor.ExtractText` 一次性得到整篇文本（`NormalizeText` 去 BOM、CRLF→LF、校验 UTF-8，保证偏移可复算），同步一次方舟总结写 `documents.summary`，原文件落盘后只存路径；**导入到此为止，不自动提炼**——资料不一定是知识，可能只是存档。提炼只由所有者在资料列表点「提炼」（`POST /materials/:id/reprocess`）触发，后台队列对整篇文本做一次方舟调用，逐条校验 Evidence 后写入 `knowledge_items`；启动时不再恢复任何待提炼资料。同一 `source_key` 新版本处理完成后，旧知识的 Evidence 标记 `superseded`，列表与检索隐藏只剩旧证据的知识，模型只看到当前 Evidence。
- `service.retrieve` 是问答、草稿、`/search` 的唯一检索入口。融合三路：知识关键词、知识语义、**资料语义**——资料命中不直接返回资料，而是把该资料提炼出的知识按 RRF 提权（同样受 scope 限制）。配置了 `ARK_EMBEDDING_MODEL` 时调用失败必须报 `ARK_*` 错误，不得悄悄退回关键词。语义门槛 `semanticFloor`/`semanticMargin` 依据实测设定，换模型或维度后必须重测再改。
- 向量：`/embeddings/multimodal`。**知识与资料各嵌一次**：知识嵌“标题+正文”（`knowledge_vectors`），资料嵌上传时生成的整篇摘要（`document_vectors`）。过期按 sha256、模型、维度判定。向量化是**纯手动**的：导入资料、提炼知识都不调用 embedding，只有 `POST /vectors/reindex`（`service/vectors.go` 的 `ReindexAllVectors`）才建向量；没有当前向量的知识或资料不贡献语义项，检索自然退化为纯关键词。
- 使用次数只有一张计数：`knowledge_items.use_count` 与 `documents.use_count`，由草稿成功生成时按本次实际使用的来源 +1（长文与图文帖都算）。随机知识、指定知识点、资料原文都计一次；只写灵感、没有来源的草稿不计数。不要恢复 `artifact_knowledge` 或任何来源关联表，也不要把它做成删除限制。
- 草稿生成成功后，除了写 SQLite，还会把渲染好的 Markdown 写到 `data/drafts/<id>-<标题>.md`（与导出 ZIP 里的 `draft.md` 相同，标题非法字符替换为 `-`）。数据库仍是唯一事实来源，写文件失败只记日志、不改变草稿状态。

### 接口与安全

- 默认监听 `127.0.0.1`，配置层只允许回环 IP，`localhost` 规范化为 `127.0.0.1`，拒绝通配、局域网或公网绑定。`internal/httpapi/guard.go` 必须保留：Host 只允许本机名；非 GET 拒绝跨站 Origin 与 `Sec-Fetch-Site: cross-site`；POST/PUT/PATCH/DELETE 只接受 `application/json`（空 body 除外；multipart 只对 `POST /materials` 与 `POST /media-drafts/:id/wechat` 放行）。`GET/PUT /wechat/settings` 不回传 AppSecret（恒为空串，`app_secret_set` 表示已保存），PUT 时 secret 留空即保留原值；代理 URL 用户名和密码替换为 `redacted`，原样保存遮蔽值保留凭据，空串清除，新地址替换。方舟上游错误不保留原始响应正文，避免进入日志或 API 错误信息。
- 带 `:id` 的资料、草稿读写都必须校验归属与状态，非法 ID 一律拒绝；草稿的修改只允许 `status=generated`，删除拒绝 `running`；资料删除（`DELETE /documents/:id`，级联删要点与向量，磁盘文件无共享才删）拒绝 `processing`，重命名（`PUT /documents/:id`）只改显示名。
- 统一 envelope：`{data, request_id}` / `{error:{code,message}, request_id, run_id?}`；新增错误码要同时更新 `httpapi/response.go` 的状态映射、前端 `describeError` 与 [README.en.md](README.en.md) 的错误表。
- 外部 AI 只用 `internal/ark`（对话 `Client`：强制 tool 的 `callTool` 与 Agent 用的自由工具调用 `Chat`；向量 `Embedder`），只用户自有火山方舟；`ARK_BASE_URL` 只允许官方 `volces.com` HTTPS；不建设多厂商 Provider、不自动降级。演进方向是本地 Agent：循环、工具、校验写在 service，不依赖厂商。
- 唯一的其他外部服务是微信公众号接口，只走 `internal/wechat`，host 固定为 `api.weixin.qq.com`。
- 只有一个知识库，没有权限或租户隔离；不同用户应各自安装。
- **全局密码（锁屏）**：可选，存 `app_lock`（PBKDF2-SHA256 210000 轮 + 随机盐，hex）。解锁令牌格式为 `<过期unix秒>.<hex(HMAC-SHA256(key=进程内随机32字节密钥, "clovergo-session|<过期>|<密码哈希>"))>`，有效期 30 天；密钥只在内存，所以重启、改密、删密都会使旧令牌失效。请求头 `X-CloverGo-Token`，前端存 `localStorage` 键 `clovergo.token`。未设密码全部放行；设了之后 `router.go` 的 `requireUnlock()` 保护除 `GET /auth/status`、`POST /auth/unlock` 外的全部 `/api/v1/*`，`/health/*` 与静态资源不受保护。连续 5 次错误锁 30 秒（内存计数，重启清零；校验在互斥锁内串行，并发猜测也不能超过 5 次）。令牌比较必须常量时间；密码与令牌不得写入日志或错误信息。这是本机锁屏，不是多用户账号：新增 `/api/v1` 路由必须挂在受保护组里。

### 密钥与仓库卫生

- 方舟 API Key 只能来自当前进程环境或 Git 忽略的 `.env`；不得进入源码、SQLite、日志、文档、命令回显、导出包、截图或提交历史。
- 公众号 AppID/AppSecret 与可选 SOCKS5 代理 URL 由所有者在界面填写，存在本机 `wechat_settings`，是本机配置而非代码密钥；它们不得出现在日志、错误信息、导出包或提交历史中。环境变量 `WECHAT_APP_ID`/`WECHAT_APP_SECRET`/`WECHAT_PROXY_URL` 只是尚未保存时的初始值，`.env` 被 Git 忽略。
- 不提交 `.env`、`.local/`、`data/` 运行内容、SQLite/WAL、可执行文件、个人资料、模型输出、`node_modules`。
- 所有代码与部署均为实验性：优先最短路径的真实实现；不预做备份、高可用、兼容层或隐藏兜底；缺配置、账号、配额时明确失败，不用 mock 冒充完成。
- Windows 下文本读写显式 UTF-8；含中文的 `.ps1` 保存为 UTF-8 BOM。

## 3. 验证与提交

- Go 修改：`gofmt -l .` 无输出、`go vet ./...`、`go build ./...` 全部通过；涉及启动或配置时跑 `scripts/doctor.ps1 -StartHealthCheck`。默认不新增自动化测试套件。
- 前端修改：`npm run build`，提交 `web/dist`，按 [docs/FRONTEND.md](docs/FRONTEND.md) 做冒烟。
- 涉及模型的改动必须用操作者的有效 Key 在临时数据库上真实验收（不得用 mock 冒充），至少覆盖：导入并检查 Evidence、补齐向量与混合检索（含跨语言、无关查询）、公众号文章草稿生成、草稿编辑与校验拒绝路径、修改平台风格后 Prompt 版本变化；记录 token 与耗时，任何输出不得打印 Key。
- 提交前检查 `git status --short`、`git diff --cached --check`、暂存内容中无 Key 与运行产物。默认只本地 commit/tag，不 push。
- 一个正式 Phase 对应一个业务提交和一个 Tag；文档整理不冒充功能 Phase。

## 4. 版本

- `v0.2.0`：统一公开基线，历史合并为唯一根提交 `Initial Commit`，只保留 `v0.2.0` 标签。包含当前资料归档、证据提炼、只读 Agent、混合检索、公众号草稿与本机安全限制；真实验证范围见 [docs/RELEASE-CHECK.md](docs/RELEASE-CHECK.md)。
- `VERSION` 是程序与构建脚本的版本来源，前端 `package.json` 和 `package-lock.json` 同步更新。源码 ZIP 无 Git 元数据时也必须显示正确版本；构建时可用 `-X main.version=...` 明确覆盖版本。
- 以下设计曾经存在并已移除，不要恢复（相应约束见第 1–2 节与 docs/BACKEND.md）：知识空间 / Industry Packs、小红书 / 今日头条 / 抖音 / 视频号、选题建议、示例范文（模板）、分块与候选审核（`chunks`/`candidates`/`knowledge_evidence`）、语义去重、草稿来源引用（`artifact_knowledge`、删除知识时的引用检查）、导入时自动提炼与自动向量化。
