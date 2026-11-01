# CloverGo 全面代码与文档审查报告

> 历史审查记录：以下问题描述保留当时状态，部分已在后续版本修复，不能作为当前待修复清单。最新开源验收范围见 [RELEASE-CHECK.md](RELEASE-CHECK.md)。

- 审查日期：2026-10-27
- 审查方式：只读。已读 Go 代码（`main.go`、`internal/**`）与 Markdown 文档，并执行 `gofmt -l`、`go vet ./...`、`go build ./...`、`go list ./...`（均无写入）。除本文件外未修改任何文件，未启动服务。
- 前端仅做只读核对（`frontend/src/lib/api.ts`、`i18n.ts`），不在审查范围内评价实现。
- 严重度：高 / 中 / 低。没有证据的怀疑一律标「待验证」。项目按 AGENTS.md 是实验性项目，本报告不建议备份、兜底或高可用方案。
- **覆盖说明**：下列文件只做了定向 Grep 或未深入阅读，结论标「未深入审查」：`service/{vectors,settings,ask}.go`、`repository/{platforms,prompts,runs,stats,vectors,wechat}.go`、`httpapi/{ask,runs,settings}.go`、`ark/{answer,embed,summary,chat}.go`、`prompts/*.go` 与 `layers/*.md` 的全文、`platforms`、`extractor`、`workflows`、`logging`、迁移 001/002 的全文、`scripts/`。

- **修复状态（2026-10-27）**：已修 M1、M2、L2、L3、L7–L9、L11–L15、L17、L19–L21、L25–L28 及 `types.ts`；未改（设计取舍或遗留命名）L1、L4–L6、L10、L16、L18、L22–L24。下文各条保留原始描述，API.md 第 6 节中 `run_id`/`text?`/WECHAT 文案/health 路径项亦已处理。

---

## 1. 概览

### 1.1 工具链结果

| 检查 | 结果 |
| --- | --- |
| `gofmt -l internal main.go` | 无输出 |
| `go vet ./...` | 无输出 |
| `go build ./...` | 通过 |
| `go list ./...` | 15 个包 |

### 1.2 包与职责（约 62 个 Go 文件，约 9000+ 行，不含 `.local`）

| 位置 | 职责 | 规模（行，约） |
| --- | --- | --- |
| `main.go`、`internal/app` | 进程、端口绑定、装配、恢复、内嵌 `web/dist`、优雅退出 | app.go ≈120 |
| `internal/config` | 环境配置与基准目录 | 167 |
| `internal/domain` | 领域类型（document/knowledge/run/media/wechat/agent/lock） | 小 |
| `internal/extractor` | 纯文本抽取与 `NormalizeText` | 未深入 |
| `internal/prompts` | 提示词层（`//go:embed`）、`CoreVersion`、组装 | compose.go 193 |
| `internal/platforms` | 平台默认、格式常量、规则、Validate/Apply | 187 |
| `internal/workflows` | 固定 Workflow 标识 | 小 |
| `internal/ark` | 方舟对话、强制 tool、Agent 自由调用、提炼、问答、文章 schema/校验、向量 | client 232、media 374 |
| `internal/wechat` | 公众号草稿箱客户端 + SOCKS5 拨号 | client 307、proxy 174 |
| `internal/imaging` | 提示词 → SVG → PNG 本地光栅化 | 293 |
| `internal/service` | 业务：ingest、queue、retrieval、agent、media、wechat、export、lock、vectors… | ingest 596、media 608、agent 432、wechat 299、vectors 259、lock 182 |
| `internal/repository` | SQLite：迁移、FTS、各表读写 | knowledge 564、media 287、sqlite 253、documents 233、runs 233 |
| `internal/httpapi` | gin 路由、guard、响应、各 handler、`lock.go` | router 151、response 131、guard 90、lock 83 |
| `internal/tools` | 只读关键词检索 `SearchSources`（41 行） | 41 |
| `scripts` | build/install/doctor（未深入） | — |

分层总体清晰：HTTP 只解析与响应，状态转换在 service，持久化在 repository，厂商 JSON 与输出校验在 ark。

---

## 2. 架构与分层对照 AGENTS.md 第 1–3 节

| 条目 | 结论 | 依据 |
| --- | --- | --- |
| 单实例单知识库，无空间/租户 | 符合 | 检索、草稿无 scope 维度 |
| 平台仅 `gongzhonghao`，article/gallery 两种格式，数字在 `internal/platforms` | 符合 | `ark/media.go:238-304` 的 `validateArticle` 全部引用 `platforms.*` 常量，未抄数字 |
| 无模板功能 | 符合 | 未发现 `draft_templates` 的代码引用 |
| 资料/知识两张表，证据内联，无 chunks/candidates | 符合（命名有遗留，见 L24） | `validateDraft` 重算 rune 偏移（`ingest.go:485-513`） |
| 导入只总结不自动提炼，提炼由 `POST /materials/:id/reprocess` 触发 | 符合 | `storeMaterial`（`ingest.go:160-250`）仅总结+落盘 |
| 整篇单次提炼，逐条校验连续原文 Evidence | 符合 | `ingest.go:305-469`、`validateDraft` |
| 固定 4 个 Workflow、Agent 最多 8 轮、仅只读工具 | 符合 | `agent.go:22`（`agentMaxSteps=8`）、5 个工具均只读 |
| Agent 回复中的 `[K<id>]` 只能是本 Run 工具返回过的 ID | 符合 | `agentCitations`（`agent.go:365-384`） |
| 服务端不保存会话，但写 `agent_qa_history` | 符合 | `recordAgentQA`（`agent.go:408-417`） |
| 推送公众号只进草稿箱，不发布，host 固定 `api.weixin.qq.com` | 符合 | `wechat/client.go` 的 `apiBase` 固定 |
| 图片推送时本地生成，无图片模型 | 符合 | `imaging`；无 rasterizer 时长文丢图、图文帖 `INVALID_STATE`（`service/wechat.go:165-189`） |
| 草稿不记录引用，成功后来源 `use_count+1` | 符合 | 未发现 `artifact_knowledge` |
| 知识 `read_count` | 符合 | 未逐行核对 handler，按路由存在判断 |
| 草稿 md 文件写 `data/drafts/<id>-<标题>.md`，失败只记日志 | 符合 | `media.go:558-577`、`593-608` |
| 默认仅监听 127.0.0.1；guard 的 Host/Origin/Content-Type 约束 | 符合 | `guard.go:20-82` |
| 统一 envelope、错误码映射 | 基本符合，3 码缺映射（见 L15） | `response.go:55-85` |
| 外部 AI 只走 `internal/ark`，`ARK_BASE_URL` 只允许 `volces.com` HTTPS | 符合 | `ark/client.go:73-96` 校验 HTTPS 与后缀；`CheckRedirect` 禁止重定向（`client.go:56-68`） |
| 启动顺序与恢复 | 符合 | `app.go` + `RecoverInterruptedWork`（`repository/sqlite.go`） |
| 迁移 001/002/003 登记于 `migrations` 切片 | 符合 | 1/2/3 均在 `sqlite.go` |
| `internal/tools`：只读 `search_sources` / `get_document` | **文档与代码不一致** | 包内只有 `SearchSources`；`get_document` 在 `service.GetSourceDocument`（`service/knowledge.go:230-245`） |
| 第 2 节“置信度” | **文档与代码不一致** | 代码里存在在用的置信度下限 `AUTO_PUBLISH_MIN_CONFIDENCE`（`ingest.go:410`），AGENTS/README 均未提及（见 L25） |
| 前端 `web/dist` 提交、离线、不注入 HTML | 未审查（前端由所有者编辑中，只核对 api.ts/i18n.ts） | — |

---

## 3. 全局密码 / 锁屏功能专项审查

涉及文件：`internal/domain/lock.go`、`internal/repository/lock.go`、`internal/repository/migrations/003_app_lock.sql`、`internal/service/lock.go`、`internal/httpapi/lock.go`、`router.go`、`response.go`、`service.go`。

### 3.1 设计与实现概要

- PBKDF2-SHA256，`lockPBKDF2Iterations=210000`，16 字节随机盐，十六进制存入 `app_lock`（`service/lock.go:18-24`、`119-128`）。
- 令牌 = HMAC-SHA256(key = 库内哈希字节, "clovergo-session")（`lock.go:177-182`），放 `X-CloverGo-Token`。
- 路由分组：`public`（仅 `/auth/status`、`/auth/unlock`，`router.go:49-51`）与 `api`（`requireJSON` + `requireUnlock`，`router.go:53`），其余业务路由，包括 `PUT /auth/password`、`POST /auth/password/remove`、`/health/queue`、tools、runs，都在 `api` 内。
- `/health/live`、`/health/ready` 与静态资源（`/`、`/assets/*filepath`、NoRoute）不受保护，不返回业务数据。
- 无密码时 `VerifyToken` 直接放行（全部开放）。

### 3.2 做得对的地方

1. 令牌与密码哈希比较都用 `subtle.ConstantTimeCompare`（`lock.go:82`、`167`），且比较的是等长十六进制串。
2. 修改/移除密码必须提供 `current` 并走同一套限流（`SetPassword` L114-118、`RemovePassword` → `checkPassword`）。
3. 改密码会换盐，旧令牌随之失效；`SetPassword` 之后会重置限流。
4. 公开路由只暴露状态与解锁；令牌、密码未进日志（`accessLog` 只记路径，`rejectRequest` 只记 method/path/host/origin）。
5. 令牌放自定义头，不是 Cookie，天然不受 CSRF 影响；`localOnly` 对全局生效，也覆盖 `/auth/unlock`。
6. 前端 `downloadFile`（`api.ts:110-149`）用 fetch+blob 带头，ZIP 导出在锁定下可用；`i18n.ts` 已覆盖 `AUTH_REQUIRED`、`PASSWORD_INCORRECT`、`TOO_MANY_ATTEMPTS`。
7. 绕过路径检查：未发现未保护的业务路由。

### 3.3 问题

**M1【中】限流“检查—校验—计数”不是原子的，并发可超过 5 次猜测** — `internal/service/lock.go:35-52`、`155-173`
`checkPassword` 先 `check()`，再做耗时的 PBKDF2（210000 轮），失败后才 `fail()` 计数。并发发出的 N 个请求会同时通过 `check()`，因此冷却触发前的猜测次数不止 5 次，上限取决于并发数。本机服务、攻击者需先能访问 127.0.0.1，实际利用门槛较高，故为中。
建议：在 `check()` 内就“预占”一次尝试（先计数，成功再 `reset()`），或用互斥锁串行化 `checkPassword`。

**M2【中】令牌确定性、永不过期，且密钥就是库内存的密码哈希** — `internal/service/lock.go:177-182`、`frontend/src/lib/api.ts`
令牌只由库内哈希与常量字符串决定：读到 SQLite 的人无需暴力破解即可算出有效令牌；令牌无有效期、无法按会话撤销（只有改密码换盐才会失效）；前端存 `localStorage`（键 `clovergo.token`），退出后令牌仍有效。README 已声明“能读数据目录的人仍可读数据”，所以对“读磁盘”的威胁是已知边界，但“令牌 = 哈希的函数”仍让该边界更窄（哈希泄露即等于登录）。
建议：启动时生成随机服务端密钥（或存一张会话表）签发带过期时间的令牌；不要用密码哈希当 HMAC key。

**L1【低】限流是全局、仅内存、无来源区分** — `lock.go:29-58`、`156`
任何本机进程反复输错即可让所有者被锁 30 秒；重启即清零；冷却期内连正确密码也被拒（L156）。属取舍，记录即可。

**L2【低】`hex.DecodeString` 错误被忽略** — `lock.go:178`
只在数据库被破坏时可能出现，此时 key 为空，令牌退化为对空 key 的 HMAC（可预测）。待验证是否可达。建议返回错误。

**L3【低】设置/移除密码没有事务；首次设置不需要 `current`** — `lock.go:106-153`
并发首次设置“最后写入者胜”；任何本机进程可在所有者未设置时先设置密码把所有者锁在外面（设计如此，需知晓）。`RemovePassword` 在无密码时静默返回 nil，而 `Unlock` 在无密码时返回 `INVALID_STATE`，行为不对称（`lock.go:94-96`、`142`）。

**L4【低】每个受保护请求都读一次数据库** — `lock.go:73-86`
`VerifyToken` 每次 `GetAppLock`。SQLite 本机读很快，可忽略；若要优化可缓存并在改密码时失效。

**L5【低】令牌存 `localStorage`** — `frontend/src/lib/api.ts`
若前端出现 XSS，令牌可被读走。AGENTS.md 已规定不注入 HTML，风险被压低，仅作记录。

**L6【低】`GET /wechat/settings` 明文返回 AppSecret 与代理密码** — `internal/httpapi/wechat.go:14`
现在受 `requireUnlock` 保护，设计上是为了设置页回显；但解锁后令牌、日志、浏览器都能看到。可改为返回掩码，保存时空值保持原值（需前端配合，前端不属本次改动范围）。

**L28【低/文档】README“`/health/*` stays open”与实际不符** — `README.md:130`、`router.go:74`
`/health/live` 与 `/health/ready` 开放，但 `/api/v1/health/queue` 在 `api` 组里，需要令牌。README:123 把它写成 `/health/queue`，容易让人以为在开放的 `/health/*` 里。

**交互说明（不计问题）**
- `requireJSON` 对错误的 Content-Type 返回 415，早于鉴权的 401（`guard.go:60-82`）。无信息泄露。
- 不带 Origin 的 curl 放行（`guard.go`），是设计。
- 锁只保护 HTTP 层，`data/` 与明文 `wechat_settings` 不受保护，README 已说明。
- `/health/ready` 公开返回 `fts_mode`、`ark_model`、`embedding_model`、`version`、`wechat_configured`、`wechat_default_cover`（`router.go:34-46`）。信息量有限，仅限本机。

---

## 4. 安全

| 项目 | 结论 | 依据 |
| --- | --- | --- |
| guard.go | 合格 | Host 白名单；非 GET 拒绝 `Sec-Fetch-Site: cross-site` 与 Origin≠Host；JSON/multipart 限制（`guard.go:20-82`） |
| 上传 | 合格 | `MaxBytesReader`（上限+1MB），`ReadAll(LimitReader)`；`filepath.Base` 取文件名；落盘用 `sha256+ext`，不使用用户路径（`upload.go:29-57`、`ingest.go:167`、`223`）；文件权限 0o644 |
| JSON 解析 | 合格 | `decodeStrictJSON`：1MB、`DisallowUnknownFields`、单对象（`upload.go:60-74`） |
| 路径 | 合格 | 草稿 md 文件名经 `fileSegment` 替换非法字符，`<id>-` 前缀避免 Windows 保留名（`media.go:593-608`）；`respondZIP` 用 `%q` 文件名与 `nosniff`（`upload.go:97`） |
| SQL 拼接 | 未发现注入 | 全部拼接为常量、`placeholders(n)`、固定 `WHERE 1=1` + 参数（`repository/vectors.go:88`、`knowledge.go:96/519`、`media.go:223-237`） |
| SSRF | 合格 | ark：HTTPS + `volces.com` 后缀 + 禁止重定向；wechat：host 固定；代理为所有者配置（`wechat.ValidateProxy`） |
| 密钥泄露（日志/错误） | 合格 | `UpstreamError.Error()` 不含 Key（Key 在 header）；wechat `do` 的错误信息不含 URL（含 token 的 query），`APIError` 不含 token；`recovery` 只记 panic+栈 |
| 导出包 | 合格 | 只含 manifest.json/draft.md/draft.json，无密钥（`export.go:51-75`） |
| 命令执行 | 合格 | `imaging` 用 `exec.LookPath`+`exec.Command`，无 shell，参数固定；文本经 `html.EscapeString` 后再进 SVG（`imaging.go:173/188`） |
| HTML 渲染 | 合格 | `RenderDraftHTML` 全程 `html.EscapeString` |
| HTTP 错误处理 | 合格 | `respondError` 对 ≥500 与 429 记 ERROR；客户端只拿 Message，不拿 Cause（`AppError.Error()` 仅返回 Message） |

安全相关问题：
- **L17【低】导出 draft.md 把主题直接写进 HTML 注释** — `internal/service/export.go:87`。主题含 `-->` 时会提前结束注释，破坏 Markdown 渲染（主题是用户输入）。建议转义或去掉该注释。
- **L20【低】代理凭据超过 255 字节会被截断** — `internal/wechat/proxy.go:104-106`（`byte(len(...))`），认证会静默出错。应在 `ValidateProxy` 中拒绝。
- **L19【低】`imaging` 外部进程无超时** — `internal/imaging/imaging.go`（`exec.Command` 而非 `CommandContext`），转换工具卡住会一直阻塞推送请求。
- **L21【低】微信客户端使用默认重定向策略** — `internal/wechat/client.go:52`。access_token 在 URL query，host 固定，风险低，但可与 ark 一样禁用重定向。
- **L14【低/待验证】服务器仅设 `ReadHeaderTimeout`/`IdleTimeout`** — `internal/app/app.go:81-82`；无 `ReadTimeout`/`MaxHeaderBytes`。本机监听，影响有限。`service.Stop()`（`app.go:113`）会等待在途模型调用且无截止时间，关停是否会卡住待验证。

---

## 5. 正确性与并发

- **队列**：`extractionQueue`（2 worker/256）与 `draftQueue`（2 worker/64）在 `queue.go`。
  - **L7【低】worker 与 `processDraftJob` 没有 panic 恢复** — `queue.go:85-99`、`162-177`，`media.go:196-221`。goroutine 内 panic 会使整个进程退出（`recovery` 中间件只覆盖 HTTP）。目前没有已知触发点，建议加 `defer recover` 并把 Run/草稿标记为失败。
  - **L8【低】`Enqueue` 队列满时阻塞调用者** — `queue.go:50-65`、`150-157`；请求线程会被拖住。
- **Run 恢复**：`RecoverInterruptedWork` 把 `running` 的 Run 标为 `RUN_INTERRUPTED`、生成中的草稿标为 `failed`、提炼中的资料退回 `ready` 或 `analysis_failed`，与 AGENTS.md 一致。
- **事务**：媒体草稿的创建/完成/编辑/删除都在事务内，且在事务里再校验状态（`repository/media.go:91-210`），`DeleteMediaDraft` 拒绝 `running`，一致性良好。连接串使用 `_txlock=immediate`。
- **L9【低】`ReprocessDocument` 存在检查后更新的竞态** — `ingest.go:112-118`。状态检查（L112）与 `UpdateDocumentProcessing`（L115）之间没有原子条件，两次并发点击可能入队两次。应把状态条件放进 UPDATE 的 WHERE。
- **L10【低】重新提炼保留旧知识，canonical_key 变化时可能产生近似重复** — `ingest.go`（`INSERT OR IGNORE`，唯一键为 `(document_id, fingerprint)`）。属于“故意不做跨资料合并”的延伸，仅作提醒。
- **迁移**：`001` 基线 + `002` 指纹作用域 + `003` app_lock，均在 `repository/sqlite.go` 登记；`ErrIncompatibleSchema` 拒绝旧库。迁移 001/002 的 SQL 全文未深入审查。
- **FTS**：`initFTS` 在代码里创建，含 trigram/unicode61/LIKE 回退。**L13【低】** `knowledge_au` 触发器在任何 UPDATE 上重建 FTS 行，`use_count`/`read_count` 自增也会触发；可限制为 `UPDATE OF title, body`（待验证触发器现状，建议核对 `initFTS`）。
- **向量与检索（RRF）**：`retrieve`（`retrieval.go:49-170`）三路融合（知识关键词、知识语义、资料语义提权），`rrfK=60`、`retrievalDepth=50`、`semanticFloor=0.40`、`semanticMargin=0.06`；配置了嵌入模型时调用失败返回 `ARK_*` 错误而不是悄悄降级（L79-81），符合约束。
  - **L22【低】向量读取与 `GetKnowledgeByIDs` 之间知识被删除会得到 500** — `retrieval.go:157`（行数不一致时返回错误）。窗口很小。
  - **L23【低】每次查询都把全部向量加载进内存再计算** — `retrieval.go:82`、`108`。个人库规模下可接受；库变大后会成为性能瓶颈。
- **Agent 循环上限**：`agentMaxSteps=8`（`agent.go:22`）。
  - **L12【低】第 8 轮执行完工具后直接返回 `AGENT_STEP_LIMIT`，没有最后一次让模型作答的机会** — `agent.go:191`。
  - **L11【低】`recordAgentQA` 失败会让整次回复失败** — `agent.go:155-157`。历史记录本是旁路功能，建议失败只记日志。
- **校验**：
  - Evidence：`validateDraft` 用 `strings.Index` 取首次出现位置并重算 rune 偏移（`ingest.go:485-513`）。若同一句话在全文出现多次，偏移指向第一处，对“连续子串”约束成立。
  - `ValidateArticle` / `validateArticle`（`ark/media.go:231-304`）：标题、摘要、引言、封面提示词、结尾字数；章节数量与 `Order` 连续；章节标题/正文字数；配图提示词与图注成对（无提示词则清空图注）；引言+章节+结尾总字数；标签数。人工编辑与模型输出共用，符合 AGENTS.md。
  - 图文帖 `validateGallery` 只读了入口（`media.go:308-315`），细节未深入审查。
- **L18【低】公众号推送无幂等，且图文帖中途失败会遗留已上传的永久素材** — `service/wechat.go:117-160`、`165-189`。重复点击会在草稿箱产生多份，`MarkDraftPushed` 只覆盖记录。
- **L16【低】`queryLimit` 不钳制** — `httpapi/upload.go:84`。service 层 `normalizeLimit` 已钳制到 ≤100，且 Agent 钳制到 1–8，所以经核对没有可达的负数 panic。
- **启动/关停**：优雅退出 10s（`app.go:103-105`），之后 `service.Stop()` 等待在途任务（见 L14）。

---

## 6. 错误码一致性

核对对象：Go 常量（`service.go:15-45`）、`httpapi/response.go:55-85` 的 `statusForCode`、README 错误表（`README.md:132-150`）、前端 `frontend/src/lib/i18n.ts` 的 `error.<CODE>`（en L180-205、zh L750-774）。

| 项目 | 结果 |
| --- | --- |
| Go 错误码数量 | 30 个 |
| README 错误表 | 30 个全部覆盖，含 `AUTH_REQUIRED`、`PASSWORD_INCORRECT`、`TOO_MANY_ATTEMPTS`（L142-144） |
| 前端 `error.*` 键 | 覆盖全部 30 个，另有前端专用 `NETWORK_ERROR`；en/zh 键对齐 |
| `statusForCode` | `AUTH_REQUIRED`、`PASSWORD_INCORRECT` → 401；`ARK_RATE_LIMITED`、`TOO_MANY_ATTEMPTS` → 429（`response.go:74-76`）。**缺少 3 个码**：`RUN_INTERRUPTED`、`FORBIDDEN_ORIGIN`、`UNSUPPORTED_MEDIA_TYPE` |

问题：
- **L15【低】`statusForCode` 缺 3 个码，落入 500** — `response.go:55-85`。`FORBIDDEN_ORIGIN`、`UNSUPPORTED_MEDIA_TYPE` 由 `guard.go` 的 `rejectRequest` 直接写入状态码，`RUN_INTERRUPTED` 只写数据库，所以目前没有实际影响；但之后若有代码通过 `respondError` 返回它们会变成 500。建议在映射表里补上（403/415/409 之类）。
- **L27【低】前端 `error.WECHAT_NOT_CONFIGURED` 文案已过时** — `i18n.ts` en L201 / zh L770 仍提示在 `.env` 设置 `WECHAT_APP_ID/SECRET` 后重启，而 AGENTS.md 规定配置在“配置 → 接口配置”界面（`.env` 只是初始值）。README:140 写的是“Configuration → Credentials”，与 AGENTS.md 的“接口配置”命名也不一致。（前端只读，仅报告。）
- `NO_GROUNDED_KNOWLEDGE`：Go 仍在使用（`ask.go:73`、`media.go:454/465`），前端文案存在，不是死码。README 写“No usable confirmed knowledge”，而 `media.go` 里还用于“所选知识只引用了已被取代的版本/不存在”，描述略窄（可忽略）。

---

## 7. 文档与代码对照

审查期间所有者已在编辑文档，核对时发现全局密码相关内容已写入，因此“待补”项实际已补：

| 项目 | 状态 |
| --- | --- |
| AGENTS.md §3 的 `service/lock.go`、`httpapi/lock.go`（行 85、87）、前端 `LockScreen`/`Security`/api.ts 令牌说明（行 99-107） | 已补 |
| AGENTS.md 迁移段落提到 `003_app_lock` | 已补（`003_app_lock` 出现 1 行） |
| `repository/lock.go`、`domain/lock.go` 在 §3 表格的 repository/domain 行 | 未核对是否列出（待补，可能缺） |
| README 能力表、API 表（L129-130）与错误表（L142-144） | 已补 |
| README.zh-CN 能力表（L45）；API/错误码表仍指向英文 README（L93） | 可接受，无需追加 |

仍存在的文档问题：
- **L25【低】`AUTO_PUBLISH_MIN_CONFIDENCE` 未在 AGENTS.md / README 记载** — 代码 `config.go:28-30`、`79`（默认 0.72）、`.env.example:20`、`scripts/doctor.ps1:117`、`ingest.go:410`（低于阈值的候选被丢弃，并写入 trace `auto_publish_min_confidence`，`ingest.go:454`）。这是一个在用的运行参数，却没有文档。注释还写着“replaces the review step”，指向已移除的审核功能。
- **L26【低】`internal/tools` 的描述与代码不一致** — AGENTS.md:88 写“只读 `search_sources`（仅关键词）/ `get_document`”，包内实际只有 `SearchSources`（`tools/local.go`，41 行），`get_document` 在 service 层。README:128 的 API 表列出 `/tools/search_sources`、`/tools/get_document` 属实（路由存在于 `knowledge.go:128/144`）。
- **L28** 见 §3.3。
- AGENTS.md 第 4 节 `/health/queue` 写法缺 `/api/v1` 前缀（实际路径是 `/api/v1/health/queue`）：待补。
- `internal/prompts/layers/*.md`（gallery、agent、persona、answer、extraction、media、summary，共 7 个）只做了清单核对，全文未深入审查；AGENTS.md 提到的“分类层与格式数字层不可编辑”与 7 个文件的对应关系待验证。
- 全库 Grep 显示已移除功能（小红书/头条/抖音/模板/`artifact_knowledge`）只出现在 AGENTS.md 的“已移除”说明里，代码中无残留。

---

## 8. 死代码与冗余

- `go vet`、`gofmt`、`go build` 均无告警。
- `internal/tools` **在用**（`service/knowledge.go:220`、`httpapi/knowledge.go:128`），不是死代码，但只是对 `repo.SearchKnowledge` 的一层薄封装，并且只有关键词检索，能力与 `retrieve` 重复，因此是冗余候选（**L26** 里一并记录）。
- `normalizeOffset`（`service/knowledge.go:268`）在 `agent.go:422`、`knowledge.go:78`、`media.go:310` 使用，在用。
- `UsefulCount`（`agent.go:332`）在用。
- **L24【低】遗留命名** — `CandidateDraft`、`ExtractCandidates`（`service.go:69`、`ark/extract.go:23`）、`maxCandidatesPerDocument`、JSON 字段 `candidates`、`autoPublishMinConfidence`、`Service` 字段名。功能还在，但与“没有 candidates/自动发布”的叙述冲突，容易误导接手者。是否改名属可选。
- `internal/ark/extract.go` 注释里的“candidates”同样是遗留用语。
- 未发现其他明确的死代码；`repository`/`service` 其余文件未做逐函数引用扫描（未深入）。

---

## 9. 问题汇总

统计：**高 0 条 / 中 2 条 / 低 28 条**（共 30 条，另有若干不计数的说明项）。

| # | 严重度 | 位置 | 摘要 |
| --- | --- | --- | --- |
| M1 | 中 | `service/lock.go:35-52,155-173` | 限流检查与计数非原子，并发可超 5 次猜测 |
| M2 | 中 | `service/lock.go:177-182` | 令牌确定性、永不过期，且以库内密码哈希为密钥 |
| L1 | 低 | `service/lock.go:29-58,156` | 全局内存限流，不区分来源，冷却期拒正确密码 |
| L2 | 低 | `service/lock.go:178` | 忽略 hex 解码错误（待验证可达性） |
| L3 | 低 | `service/lock.go:106-153` | 设置/移除无事务；首次设置无需 current；两处行为不对称 |
| L4 | 低 | `service/lock.go:73-86` | 每个受保护请求读一次库 |
| L5 | 低 | `frontend/src/lib/api.ts` | 令牌存 localStorage |
| L6 | 低 | `httpapi/wechat.go:14` | GET 接口配置明文返回 AppSecret 与代理密码 |
| L7 | 低 | `queue.go:85-99,162-177`；`media.go:196-221` | worker 无 panic 恢复，panic 会杀进程 |
| L8 | 低 | `queue.go:50-65,150-157` | 队列满时 Enqueue 阻塞 |
| L9 | 低 | `ingest.go:112-118` | ReprocessDocument 检查与更新之间有竞态 |
| L10 | 低 | `ingest.go`（INSERT OR IGNORE） | 重提炼可能产生近似重复知识 |
| L11 | 低 | `agent.go:155-157` | 历史记录写失败导致整次回复失败 |
| L12 | 低 | `agent.go:191` | 第 8 轮执行工具后无作答机会 |
| L13 | 低 | `repository/sqlite.go`（`knowledge_au`） | 计数自增也触发 FTS 重建（待验证触发器范围） |
| L14 | 低 | `app/app.go:81-82,113` | 缺 ReadTimeout/MaxHeaderBytes；Stop 无等待上限（待验证） |
| L15 | 低 | `httpapi/response.go:55-85` | 3 个错误码缺状态映射 |
| L16 | 低 | `httpapi/upload.go:84` | queryLimit 不钳制（service 层已钳制，无可达问题） |
| L17 | 低 | `service/export.go:87` | 主题写入 HTML 注释，含 `-->` 可打断 |
| L18 | 低 | `service/wechat.go:117-189` | 推送无幂等；图文帖失败遗留永久素材 |
| L19 | 低 | `imaging/imaging.go` | 外部进程无超时 |
| L20 | 低 | `wechat/proxy.go:104-106` | 代理账号/密码 >255 字节被截断 |
| L21 | 低 | `wechat/client.go:52` | 默认跟随重定向（host 固定，风险低） |
| L22 | 低 | `retrieval.go:157` | 读向量与取知识之间被删除会得 500 |
| L23 | 低 | `retrieval.go:82,108` | 每次查询全量加载向量 |
| L24 | 低 | `ark/extract.go`、`service.go:69`、`config.go:28-30` | 遗留命名（candidates / AutoPublish） |
| L25 | 低 | `config.go:79`、`ingest.go:410`、`.env.example:20` | `AUTO_PUBLISH_MIN_CONFIDENCE` 未在文档中记载 |
| L26 | 低 | `internal/tools`；AGENTS.md:88 | 文档写 get_document，包内没有；包本身是薄封装 |
| L27 | 低 | `frontend/src/lib/i18n.ts` L201/L770；README:140 | `WECHAT_NOT_CONFIGURED` 文案过时，与 AGENTS.md 的“接口配置”命名不一致 |
| L28 | 低 | `README.md:123,130` | “/health/* stays open”与 `/api/v1/health/queue` 受保护不符 |

### 建议修复优先级

1. **先修 M1 与 M2**（锁功能的两个核心缺陷）：限流改成预占式；令牌改为随机服务端密钥或会话表并带过期。
2. **L7、L9、L11**：worker 加 `recover`；`ReprocessDocument` 把状态条件写进 UPDATE；`recordAgentQA` 失败只记日志。
3. **L15、L17、L20、L2**：改动小、几行即可（补状态映射、转义注释、拒绝超长代理凭据、处理 hex 错误）。
4. **L6、L18、L19**：接口配置回显掩码、推送去重提示、给 imaging 加超时。
5. **文档类（L25-L28、AGENTS.md 里 `/health/queue` 路径、repository/domain 锁文件列表）**：随所有者当前的文档编辑一起处理。
6. 其余低优先级项（L1、L3-L5、L8、L10、L12-L14、L16、L21-L24）按需，多数是取舍或规模变大后才有影响。

> 说明：本报告不含对 `ark/{answer,embed,summary,chat}.go`、`prompts` 全文、`layers/*.md` 全文、迁移 001/002 全文、`scripts/` 的深入结论；这些区域未发现问题只表示未深入审查，不代表已确认无问题。
