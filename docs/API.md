# CloverGo HTTP API（前端对接文档）

> 依据真实代码整理：`internal/httpapi/router.go` 及各 handler、`response.go`、`guard.go`、`internal/domain/*.go`，并对照 `frontend/src/lib/types.ts`、`frontend/src/lib/api.ts`。
> 路由总数 **49 条**：根路径 health 2 条 + `/api/v1` 下 47 条（公开 2 + 受保护 45）。另有静态资源 `/`、`/assets/*filepath`。

## 目录

1. 通用约定
2. 错误码总表
3. 接口清单
   - 3.1 健康
   - 3.2 认证（全局密码）
   - 3.3 资料与上传
   - 3.4 知识 / 主题 / 检索
   - 3.5 问答 ask
   - 3.6 Agent 与历史
   - 3.7 只读工具 tools
   - 3.8 Runs
   - 3.9 平台与风格
   - 3.10 提示词层
   - 3.11 向量化 / 统计 / 队列 / Workflows
   - 3.12 公众号设置
   - 3.13 草稿 media-drafts 与推送
4. 长任务与轮询速查
5. types.ts 缺失类型（TS 建议）
6. 已知不一致
7. 附录：前端 api.ts 用法

---

## 1. 通用约定

### 1.1 基址

- 默认 `http://127.0.0.1:8088`；开发时前端 `127.0.0.1:5173` 把 `/api` 与 `/health` 代理到 8088。
- 业务接口前缀 `/api/v1`；健康检查 `/health/live`、`/health/ready` 在**根路径**。
- 未知路由：404 `{"error":{"code":"NOT_FOUND",...}}`。

### 1.2 响应 envelope

成功：

```json
{ "data": <任意>, "request_id": "..." }
```

失败：

```json
{ "error": { "code": "INVALID_INPUT", "message": "..." }, "request_id": "...", "run_id": "run_xxx" }
```

- `run_id` 仅在失败的 ask / agent / 草稿等已建立 Run 的调用里出现。
- 响应头 `X-Request-ID` 与 `request_id` 一致。
- 例外：`GET /media-drafts/:id/export` 成功时直接返回 ZIP 二进制，不是 envelope；失败仍是 envelope。

### 1.3 Content-Type 与跨站限制（`guard.go`）

- `Host` 只允许 `127.0.0.1` / `localhost` / `::1`（及监听地址的 host），否则拒绝。
- 非 GET/HEAD/OPTIONS 的请求：带 `Sec-Fetch-Site: cross-site`，或带 `Origin` 且与 `Host` 不同 → 403 `FORBIDDEN_ORIGIN`。
- POST/PUT/PATCH/DELETE 只接受：
  - `Content-Type: application/json`；
  - `multipart/form-data`：只对 `POST /api/v1/materials` 与 `POST /api/v1/media-drafts/:id/wechat` 放行；
  - **空 body 且无 Content-Type**（`Content-Length: 0`）也放行（如 `reprocess`、`read`、`delete`、`reset` 等）。
  - 其他 → 415 `UNSUPPORTED_MEDIA_TYPE`。
- JSON 请求体解析（`decodeStrictJSON`）：最大 1 MiB；**拒绝未知字段**；必须恰好一个 JSON 对象；不合法 → 400 `INVALID_INPUT`。
- 路径 `:id` 必须为正整数，否则 400 `INVALID_INPUT`。

### 1.4 全局密码鉴权

- 请求头：`X-CloverGo-Token: <token>`。token 来自 `POST /api/v1/auth/unlock` 或 `PUT /api/v1/auth/password` 的返回；前端存 `localStorage["clovergo.token"]`。
- **未设置密码时全部放行**；设置后，除下列路径外全部要求 token，否则 401 `AUTH_REQUIRED`。
- 不受保护：`/health/*`、静态资源、`GET /api/v1/auth/status`、`POST /api/v1/auth/unlock`。
- 本文档每个接口标注「公开」或「受保护」。
- 密码 4–64 rune；token = HMAC-SHA256(密码哈希, "clovergo-session") 的 hex，改/删密码后旧 token 失效。
- 连续 5 次错误 → 冷却 30 秒（内存态），期间返回 429 `TOO_MANY_ATTEMPTS`。
- **ZIP 导出不能用 `<a href>`**（带不了头），要用带头 fetch，前端已有 `downloadFile`（见附录）。

### 1.5 分页约定

- 列表 `limit` / `offset` 查询参数；`offset<0` 视为 0；`limit<=0` 取各接口默认值，`>100` 取 100（Runs 最大 50）。
- 分页响应形如 `{ "items": [...], "total": N }`（TS：`Paged<T>`）。`GET /documents`、`GET /runs`、`GET /topics`、`GET /kinds` 等不分页。

### 1.6 长任务

见第 4 节。要点：提炼（reprocess，202）与草稿生成（media-drafts，201）是后台异步，需轮询；导入、ask、agent、reindex 是同步长请求（数秒到数分钟）。

---

## 2. 错误码总表

HTTP 状态映射位于 `internal/httpapi/response.go`。前端文案键：`error.<CODE>`（`frontend/src/lib/i18n.ts`），`describeError` 优先使用该键。

| Code | HTTP | 含义 | 前端 i18n |
| --- | --- | --- | --- |
| INVALID_INPUT | 400 | 参数/JSON/ID 不合法，或校验失败 | 有 |
| DOCUMENT_EMPTY | 400 | 资料内容为空 | 有 |
| TOOL_INPUT_INVALID | 400 | 工具入参不合法（如 limit>20） | 有 |
| AUTH_REQUIRED | 401 | 已设密码但缺/错 token | 有 |
| PASSWORD_INCORRECT | 401 | 密码错误 | 有 |
| FORBIDDEN_ORIGIN | 403 | 跨站请求被拒 | 有 |
| NOT_FOUND | 404 | 未知路由（仅 NoRoute） | **无**（缺失） |
| DOCUMENT_NOT_FOUND | 404 | 资料不存在 | 有 |
| KNOWLEDGE_NOT_FOUND | 404 | 知识不存在 | 有 |
| SOURCE_NOT_FOUND | 404 | 原文来源不存在 | 有 |
| RUN_NOT_FOUND | 404 | Run 不存在 | 有 |
| ROOT_NOT_FOUND | 404 | 资料文件夹不存在 | 有 |
| MEDIA_DRAFT_NOT_FOUND | 404 | 草稿不存在 | 有 |
| INVALID_STATE | 409 | 当前状态不允许（处理中再提炼、非 generated 改/导出/推送、未设密码解锁、无 SVG 光栅化工具推图文帖等） | 有 |
| REVISION_CONFLICT | 409 | `if_revision` 与服务端不一致（乐观锁） | 有 |
| FILE_TOO_LARGE | 413 | 上传/封面超限 | 有 |
| UNSUPPORTED_FORMAT | 415 | 非 UTF-8 .md/.markdown/.txt、损坏的 .docx；封面非 JPEG/PNG | 有 |
| UNSUPPORTED_MEDIA_TYPE | 415 | 请求 Content-Type 不被接受 | 有 |
| NO_GROUNDED_KNOWLEDGE | 422 | 没有可依据的知识（问答/草稿） | 有 |
| AGENT_STEP_LIMIT | 422 | Agent 超过 8 轮模型调用 | 有 |
| ARK_RATE_LIMITED | 429 | 方舟限流 | 有 |
| TOO_MANY_ATTEMPTS | 429 | 密码错误过多，冷却中 | 有 |
| INTERNAL_ERROR | 500 | 服务内部错误（未知错误也归此） | 有 |
| ARK_UPSTREAM_ERROR | 502 | 方舟上游错误 | 有 |
| ARK_AUTH_FAILED | 502 | 方舟鉴权失败（Key 无效） | 有 |
| ARK_INVALID_RESPONSE | 502 | 方舟输出不合规（引用越界/校验失败） | 有 |
| WECHAT_UPSTREAM_ERROR | 502 | 公众号接口错误 | 有 |
| ARK_NOT_CONFIGURED | 503 | 未配置方舟 Key/模型 | 有 |
| EMBEDDING_NOT_CONFIGURED | 503 | 未配置 `ARK_EMBEDDING_MODEL` | 有 |
| WECHAT_NOT_CONFIGURED | 503 | 未配置公众号 AppID/Secret | 有（文案已过时，见第 6 节） |
| ARK_TIMEOUT | 504 | 方舟超时 | 有 |
| RUN_INTERRUPTED | — | 仅用于把重启时残留的 `running` Run 标记；出现在 Run 的 `error_code`，不作为 HTTP 状态 | 有 |

前端客户端伪码（不来自后端）：`NETWORK_ERROR`（status 0，有 i18n）、`HTTP_ERROR`、`CLIENT_ERROR`。另有 `error.httpFailed`。

---

## 3. 接口清单

图例：🔓 公开 / 🔒 受保护（设了密码需 `X-CloverGo-Token`）。TS 类型名来自 `frontend/src/lib/types.ts`；标「建议」的表示 types.ts 缺失，定义见第 5 节。

### 3.1 健康

#### GET `/health/live` 🔓（根路径）

```json
{ "status": "live" }
```

#### GET `/health/ready` 🔓（根路径）

TS：`Health`。字段：`status, fts_mode, ark_configured, ark_model, version, embedding_configured, embedding_model, wechat_configured, wechat_default_cover`。失败 500 `INTERNAL_ERROR`。

```json
{ "status": "ready", "fts_mode": "fts5", "ark_configured": true, "ark_model": "doubao-...",
  "version": "0.2.0", "embedding_configured": true, "embedding_model": "doubao-embedding-...",
  "wechat_configured": false, "wechat_default_cover": false }
```

### 3.2 认证（全局密码）

#### GET `/api/v1/auth/status` 🔓

TS：`AuthStatus`。

```json
{ "enabled": true, "unlocked": false }
```

`enabled`：是否设置了密码；`unlocked`：本次请求携带的 token 是否有效（未设密码时视为 true）。

#### POST `/api/v1/auth/unlock` 🔓

请求：`{ "password": "string" }`。响应（建议 `AuthToken`）：`{ "token": "..." }`。
错误：未设密码 409 `INVALID_STATE`；`PASSWORD_INCORRECT` 401；`TOO_MANY_ATTEMPTS` 429。

#### PUT `/api/v1/auth/password` 🔒

请求：`{ "current": "旧密码（已有密码时必填）", "password": "新密码 4–64 rune" }`。
响应（建议 `AuthPasswordResult`）：`{ "enabled": true, "token": "新 token" }`，前端须 `setToken(token)`。
错误：`INVALID_INPUT`（长度）、`PASSWORD_INCORRECT`、`TOO_MANY_ATTEMPTS`。

#### POST `/api/v1/auth/password/remove` 🔒

请求：`{ "current": "当前密码" }`。响应：`{ "enabled": false }`。前端应 `setToken("")`。
错误：`PASSWORD_INCORRECT`、`TOO_MANY_ATTEMPTS`。

### 3.3 资料与上传

资料（documents）是归档单位；导入**只归档**（同步一次方舟总结），**不自动提炼**；提炼由所有者显式触发 `reprocess`。

Document 字段（TS：`DocumentItem`）：`id, filename, source_key, version, sha256, media_type, language, topic, summary?, status, error_message?, attempts, useful_count, use_count, processing_run_id?, root_id, rel_path?, missing, created_at, processed_at?`（`root_id`/`rel_path`/`missing` 仅文件夹扫描导入的资料有意义）。

`status`（TS：`DocumentStatus`）：`ready`（已归档，未提炼）→ `processing`（提炼中）→ `published`（有知识）/ `processed_empty`（无可提炼）/ `analysis_failed`（累计失败达 3 次）。

#### POST `/api/v1/materials` 🔒

`multipart/form-data`：`file`（必填，.md/.markdown/.txt/.docx；文本类须 UTF-8，docx 转纯文本后存储）、`topic`（可选，书架/主题；为空时按识别语言默认：ja→"日语"、en→"英语"、其余空）。
上限：`MAX_UPLOAD_MB`（默认 10）+1MiB 容器；正文 `MAX_DOCUMENT_RUNES`（默认 200000）。
响应：**201** `{ "document": Document, "duplicate": false }`；内容重复（同 sha256）**200** `duplicate:true`。TS：`MaterialImportResult`。
同步长请求（含一次整篇方舟总结），需 ARK 已配置。
错误：`ARK_NOT_CONFIGURED`(503)、`UNSUPPORTED_FORMAT`、`FILE_TOO_LARGE`、`DOCUMENT_EMPTY`、`INVALID_INPUT`（非 UTF-8 / 缺 `file`）、`ARK_*`。
前端：`api("/api/v1/materials", {method:"POST", body: formData})`，**不要手设 Content-Type**。

#### POST `/api/v1/materials/text` 🔒

请求：`{ "text": "string(必填)", "topic": "string" }`。响应同上（201/200）。空文本 400 `DOCUMENT_EMPTY`。

#### POST `/api/v1/materials/:id/reprocess` 🔒

触发提炼（空 body，无需 Content-Type）。响应 **202** `MaterialImportResult`（`document.status` 通常变为 `processing`）。后台队列整篇单次提炼，逐条校验 Evidence。
轮询：`GET /documents/:id`（看 `status`）、`GET /stats`（`processing`、`pending`）、`GET /health/queue`。
错误：`DOCUMENT_NOT_FOUND`、`INVALID_STATE`（已在 processing）、`ARK_NOT_CONFIGURED`。

#### 资料文件夹（扫描导入） 🔒

登记本机文件夹，只读扫描后由前端逐个文件导入。扫描不调用模型、不导入；不跟随符号链接，跳过 `.`/`~$` 开头项与 `node_modules`，最多 5000 个文件（超出 `truncated:true`）。只处理 `.md/.markdown/.txt/.docx`。

- `GET /api/v1/roots` → `LibraryRoot[]`（`id, path, name, created_at, document_count`）。
- `POST /api/v1/pick-folder`（无 body）→ `{ "path": "..." }`：在本机弹出系统「选择文件夹」对话框（仅 Windows；其他系统 `INVALID_STATE`）；`path` 为空串表示取消。只返回路径，登记仍走 `POST /roots`。
- `POST /api/v1/roots` 请求 `{ "path": "绝对路径", "name": "可选" }` → **201** `LibraryRoot`。路径须是已存在目录，重复登记 `INVALID_INPUT`。
- `DELETE /api/v1/roots/:id` → `{ "deleted": true }`（已导入的资料保留）。错误 `ROOT_NOT_FOUND`。
- `POST /api/v1/roots/:id/scan`（空 body）→ `ScanReport`：`root, new[], changed[], skipped[], missing[], unchanged, known, other, truncated, error_dirs`；条目 `ScanEntry`：`rel_path, size, reason?, document_id?`。`skipped.reason`：`too_large`（超 `MAX_UPLOAD_MB`）/`too_long`（转换后超 `MAX_DOCUMENT_RUNES`）/`empty`/`invalid_utf8`/`invalid_docx`/`unreadable`。`known` 恒为 0（保留字段；同内容不同路径按新增处理）；`truncated` = 超过遍历上限（50000 项 / 5000 个文件）提前停止；`error_dirs` = 无法读取的子目录，其下资料不会被标记消失；`missing` = 此前导入、现已不在文件夹里的资料（同时置 `documents.missing`）。
- `POST /api/v1/roots/:id/import` 请求 `{ "path": "相对根目录的路径", "topic": "" }` → 同 `POST /materials`（201/200 `MaterialImportResult`）。`path` 必须在根内（拒绝 `..`、符号链接、非常规文件）；变更的文件成为同一 `source_key`（`root<ID>/<小写相对路径>`）的新版本。docx 转纯文本后存为 `.txt` 快照。需 ARK 已配置。

#### GET `/api/v1/documents` 🔒

无分页参数，返回全部。响应：`DocumentItem[]`。

#### GET `/api/v1/documents/:id` 🔒

响应（建议 `DocumentDetail`）：`{ "document": DocumentItem, "knowledge": KnowledgeItem[] }`。错误：`DOCUMENT_NOT_FOUND`。

#### GET `/api/v1/documents/:id/text` 🔒

读回原文件规范化后的整篇文本（纯文本渲染，勿注入 HTML）。TS：`SourceDocument` `{ document_id, document_name, topic, text }`。错误：`DOCUMENT_NOT_FOUND`、`SOURCE_NOT_FOUND`。

#### PUT `/api/v1/documents/:id/topic` 🔒

请求：`{ "topic": "string" }`（空串=未归类，空白折叠）。响应：`DocumentItem`。错误：`DOCUMENT_NOT_FOUND`、`INVALID_INPUT`。

#### PUT `/api/v1/documents/:id` 🔒

重命名显示名（不影响 source_key / 版本 / Evidence）。请求：`{ "filename": "string" }`（空白折叠，空或超过 200 rune → `INVALID_INPUT`）。响应：`DocumentItem`。错误：`DOCUMENT_NOT_FOUND`、`INVALID_INPUT`。

#### DELETE `/api/v1/documents/:id` 🔒

删除资料：级联删除其要点、向量与 FTS 索引；磁盘原文件仅在没有其他资料共用同一路径时删除。响应：`{ "deleted": true }`。错误：`DOCUMENT_NOT_FOUND`、`INVALID_STATE`（提炼中）。

### 3.4 知识 / 主题 / 检索

KnowledgeItem（TS：`KnowledgeItem`）：`id, document_id, kind, title, body, tags[], metadata, created_at, updated_at, evidence[], topics[], use_count, read_count, match?("keyword"|"semantic"|"both"), score?`。
Evidence（TS：`Evidence`）：`document_id, document_name, topic, quote, quote_start, quote_end, document_version, superseded`（rune 偏移）。

#### GET `/api/v1/kinds` 🔒

响应：`KindInfo[]` `{ id, label, hint }`。内置：`concept, fact, procedure, parameter, tip, entity, faq` + 在用自定义分类（自定义项 `label=id`）。界面不得硬编码分类。

#### GET `/api/v1/knowledge` 🔒

查询参数：`kind`（小写、≤24 rune、无控制字符，否则 400）、`topic`、`language`、`usage`（`""`|`unused`|`used`）、`limit`（默认 20，最大 100）、`offset`。
响应：`Paged<KnowledgeItem>`。

```json
{ "items": [ { "id": 12, "document_id": 5, "kind": "fact", "title": "...", "body": "...", "tags": ["a"],
  "evidence": [ { "quote": "...", "quote_start": 10, "quote_end": 42, "superseded": false } ],
  "topics": ["日语"], "use_count": 1, "read_count": 3 } ], "total": 1 }
```

#### GET `/api/v1/knowledge/:id` 🔒

响应：`KnowledgeItem`。错误：`KNOWLEDGE_NOT_FOUND`。

#### PUT `/api/v1/knowledge/:id` 🔒

请求（建议 `KnowledgeUpdateInput`）：`{ "kind": "必填 ≤24", "title": "string", "body": "非空", "tags": ["≤8 个，每个 ≤32 rune，去重"] }`。响应：`KnowledgeItem`。错误：`INVALID_INPUT`、`KNOWLEDGE_NOT_FOUND`。

#### DELETE `/api/v1/knowledge/:id` 🔒

响应（建议 `DeleteResult`）：`{ "id": 12, "status": "deleted" }`。

#### POST `/api/v1/knowledge/:id/read` 🔒

展开原文证据时记一次已读（空 body）。响应（建议 `ReadResult`）：`{ "id": 12, "read_count": 4 }`。

#### GET `/api/v1/topics` 🔒

响应：`TopicSummary[]` `{ topic, knowledge_count }`。

#### GET `/api/v1/search` 🔒

查询：`q`（必填）、`limit`（默认 20，最大 100）、`topic`、`language`。检索 = 关键词 + 豆包语义（RRF 融合；无向量时退化为关键词；配置了 embedding 而调用失败返回 `ARK_*`）。
响应：`KnowledgeItem[]`（含 `match`、`score`）。错误：`INVALID_INPUT`（缺 q）、`ARK_*`、`EMBEDDING_NOT_CONFIGURED` 不会出现（未配置时仅关键词）。

### 3.5 问答 ask

#### POST `/api/v1/ask` 🔒

请求：`{ "question": "1–500 rune（trim 后）" }`。**同步长请求**。
响应：TS `Answer`（后端字段：`run_id, answer, citations`）：

```json
{ "run_id": "run_ab12...", "answer": "……[K12]……",
  "citations": [ { "knowledge_id": 12, "title": "...", "evidence": [ { "quote": "..." } ] } ] }
```

只引用本次上下文的 Knowledge ID。错误：`INVALID_INPUT`、`NO_GROUNDED_KNOWLEDGE`(422)、`ARK_*`、`ARK_INVALID_RESPONSE`（引用越界/无引用）。失败响应带 `run_id`。

### 3.6 Agent 与历史

#### POST `/api/v1/agent/chat` 🔒

请求：`{ "messages": [ { "role": "user"|"assistant", "content": "1–4000 rune" } ] }`，1–20 条，**末条必须是 user**；服务端不保存会话，客户端每轮全量发回。
**同步长请求**：最多 8 轮模型调用，只读工具：`search_knowledge(query, limit 1–8, topic?)`、`get_knowledge`、`get_document`、`list_documents`、`library_overview`。回复中的 `[K<id>]` 只能是本次 Run 工具返回过的 ID；违规时服务端告知允许的 ID 并重试一次，仍违规则整次失败。成功后写入 `agent_qa_history`。
响应：TS `AgentReply`：

```json
{ "run_id": "run_...", "reply": "……[K3]", "citations": [ { "knowledge_id": 3, "title": "...", "evidence": [] } ],
  "steps": [ { "tool": "search_knowledge", "arguments": "{\"query\":\"x\"}", "summary": "命中 3 条" } ] }
```

错误：`INVALID_INPUT`、`AGENT_STEP_LIMIT`(422)、`ARK_*`、`ARK_INVALID_RESPONSE`。失败带 `run_id`。

#### GET `/api/v1/agent/history` 🔒

查询：`limit`（默认 20，最大 100）、`offset`。响应：`Paged<AgentQA>`，`AgentQA`：`id, run_id, question, answer, citations, steps, model, prompt_version, created_at`。

### 3.7 只读工具 tools

#### POST `/api/v1/tools/search_sources` 🔒

请求：`{ "query": "string", "limit": 5 }`（默认 5，>20 → 400 `TOOL_INPUT_INVALID`）。仅关键词检索。响应：`KnowledgeItem[]`。

#### POST `/api/v1/tools/get_document` 🔒

请求：`{ "document_id": 5 }`。响应：`SourceDocument`。错误：`DOCUMENT_NOT_FOUND`、`SOURCE_NOT_FOUND`、`TOOL_INPUT_INVALID`。

### 3.8 Runs

AgentRun（TS：`AgentRun`）：`run_id, purpose, status("running"|"succeeded"|"failed"), latency_ms, error_code?, error_message?, token_usage?, created_at, completed_at?, events[]`；`AgentEvent`：`id, ordinal, name, status, latency_ms, error_code?, error_message?, details?, created_at`。

#### GET `/api/v1/runs` 🔒

查询：`limit`（默认 5，最大 50）。响应：`AgentRun[]`（含 events，新→旧）。

#### GET `/api/v1/runs/:run_id` 🔒

响应：`AgentRun`。错误：`RUN_NOT_FOUND`。

### 3.9 平台与风格

只有一个平台 `gongzhonghao`，两种格式 `article` / `gallery`。数字规则只在后端常量，前端用接口返回的 `rules`/`gallery_rules` 渲染。

Platform（TS：`Platform`）：`id, name, format, formats, description, style_rules, revision, is_default, defaults{style_rules}, rules[{label,value}], gallery_rules?[], gallery_image_max(3), gallery_image_default(1), style_rules_max(3000)`。

#### GET `/api/v1/platforms` 🔒

响应：`Platform[]`。

#### PUT `/api/v1/platforms/:id/style` 🔒

请求：`{ "style_rules": "必填 ≤3000 rune", "if_revision": 3, "note": "≤200 rune" }`。响应：`Platform`（`revision` 递增）。
错误：未知平台 400 `INVALID_INPUT`、`REVISION_CONFLICT`(409)。

#### POST `/api/v1/platforms/:id/style/reset` 🔒

请求：`{ "if_revision": 3 }`。恢复默认风格，响应 `Platform`。错误同上。

### 3.10 提示词层

#### GET `/api/v1/prompts` 🔒

最终 Prompt 预览，TS `PromptView[]`：`{ call, title, version, sha256, text, layers:[{ key?, name, source, editable, edit_target, text }] }`。`call` 包含 summary / extraction / answer / agent 与每个平台的 draft。界面不得自行拼接提示词。

#### GET `/api/v1/prompts/layers` 🔒

TS `PromptLayerEdit[]`：`{ key, name, default_text, text, revision, customized, updated_at }`。

#### PUT `/api/v1/prompts/layers/:key` 🔒

请求：`{ "text": "1–12000 rune", "if_revision": 0 }`。整层替换。响应 `PromptLayerEdit`。错误：未知 key 400、`REVISION_CONFLICT`。

#### POST `/api/v1/prompts/layers/:key/reset` 🔒

请求：`{ "if_revision": 2 }`。恢复内置文本。响应 `PromptLayerEdit`。

### 3.11 向量化 / 统计 / 队列 / Workflows

#### POST `/api/v1/vectors/reindex` 🔒

请求体可为空，或 `{ "limit": 200 }`（≤200，默认 200；严格，多余字段 400）。**同步**；纯手动补齐知识与资料向量。
响应：`ReindexResult`（types.ts 缺 `run_id`）：

```json
{ "run_id": "run_...", "model": "doubao-embedding-...", "embedded": 18, "failed": 0, "remaining": 40, "first_error": "" }
```

`remaining>0` 时需再次调用。错误：`EMBEDDING_NOT_CONFIGURED`(503)；全部失败时返回对应 `ARK_*`。

#### GET `/api/v1/stats` 🔒

TS `LibraryStats`：`documents, knowledge, vectors, document_vectors, document_vectors_total, pending, processing`。`pending` 是「已归档未提炼」的静止数量；顶栏「提炼中」徽标按 `processing` 判断。`knowledge` 与 `vectors` 不计只剩被新版本取代（superseded）证据的知识。

#### GET `/api/v1/health/queue` 🔒

注意：在 `/api/v1` 下且受保护（区别于根路径 health）。响应（建议 `QueueStatus`）：`{ pending, draft_pending, vector_pending, document_vector_pending }`。

#### GET `/api/v1/workflows` 🔒

响应（建议 `WorkflowDef[]`）：`[{ "key": "knowledge.ingest", "version": "1.0.0" }, ...]`，包含 `knowledge.ingest / knowledge.answer / media.draft / agent.chat`。

### 3.12 公众号设置

WechatSettings（TS：`WechatSettings`）：`app_id, app_secret, app_secret_set, author, thumb_media_id, proxy_url, updated_at`。**secret 不回传**：`app_secret` 恒为 `""`，`app_secret_set` 表示是否已保存。`proxy_url` 含密码，属本机配置，不要写日志。未保存时返回环境变量种子值。

#### GET `/api/v1/wechat/settings` 🔒

响应：`WechatSettings`。

#### PUT `/api/v1/wechat/settings` 🔒

请求同上字段（严格，不含 `app_secret_set`）：`app_id` 非空而 `app_secret` 为空时保留已保存的 secret（尚无 secret 则 `INVALID_INPUT`）；两者都为空=清空并禁用推送；`author` ≤32 rune；`thumb_media_id` ≤200 rune；`proxy_url` 必须为 `socks5://user:pass@host:port`（可空）。响应：保存后的 `WechatSettings`。错误：400 `INVALID_INPUT`。

#### POST `/api/v1/wechat/settings/test` 🔒

用 `stable_token` 检查连通性（空 body）。响应：`{ "status": "ok" }`。错误：`WECHAT_NOT_CONFIGURED`(503)、`WECHAT_UPSTREAM_ERROR`(502；常见原因：IP 不在白名单)。

### 3.13 草稿 media-drafts 与推送

MediaDraft（TS：`MediaDraft`）：`id, run_id, artifact_id, platform, format("article"|"gallery"), topic, angle, audience, article?, gallery?, model, prompt_version, status("running"|"generated"|"failed"), error_message, created_at, wechat_media_id, wechat_pushed_at?, wechat_publish_id, wechat_published_at?`。
ArticleContent：`title, summary, lead, cover_image_prompt, sections[{order, heading, body, image_prompt?, image_caption?}], conclusion, tags[]`。
GalleryContent：`title, caption, images[{order, prompt}]`。
草稿不再记录引用；配图均为「提示词」，推送时才本地渲染。

#### POST `/api/v1/media-drafts` 🔒

请求（建议 `GenerateDraftInput`）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| platform | string | 必填，`gongzhonghao` |
| format | string | `article`（默认）/ `gallery` |
| image_count | number | 仅 `gallery`：图片张数 1–3，缺省 1，越界 400 `INVALID_INPUT` |
| topic | string | 创作灵感，≤500 rune |
| angle | string | ≤500 rune |
| audience | string | ≤200 rune，默认「对相关知识有兴趣的中文读者」 |
| knowledge_ids | number[] | ≤1 个 |
| document_ids | number[] | ≤1 个 |
| random_knowledge | boolean | 随机知识点 |

`topic/angle/audience/来源` 至少一项，否则 400 `INVALID_INPUT`。来源取舍优先级：`knowledge_ids` > `document_ids` > `random_knowledge`。
**异步**：立即返回 **201** `MediaDraft`（`status:"running"`），后台生成；轮询 `GET /media-drafts/:id` 直至 `generated` 或 `failed`（失败有 `error_message`）。成功后来源知识/资料 `use_count+1`，并写 `data/drafts/<id>-<标题>.md`。
错误：`INVALID_INPUT`、`NO_GROUNDED_KNOWLEDGE`(422)、`ARK_NOT_CONFIGURED`(503)。

```json
{ "platform": "gongzhonghao", "format": "article", "topic": "如何开始记笔记", "knowledge_ids": [12] }
```

#### GET `/api/v1/media-drafts` 🔒

查询：`platform`、`limit`（默认 20）、`offset`。响应：`Paged<MediaDraft>`。

#### GET `/api/v1/media-drafts/:id` 🔒

响应：`MediaDraft`。错误：`MEDIA_DRAFT_NOT_FOUND`。

#### PUT `/api/v1/media-drafts/:id` 🔒

请求（建议 `MediaDraftBody`）：`{ "article": ArticleContent }` 或 `{ "gallery": GalleryContent }`。仅 `status=generated` 可改，否则 409 `INVALID_STATE`；校验与模型输出同一套（标题/摘要/引言/结尾字数、章节数量与顺序、总字数、配图与图注成对、标签数量），失败 400 `INVALID_INPUT`。响应：`MediaDraft`。

#### DELETE `/api/v1/media-drafts/:id` 🔒

响应：`{ "id": 3, "status": "deleted" }`。拒绝 `running`（409 `INVALID_STATE`）。

#### GET `/api/v1/media-drafts/:id/export` 🔒

响应：二进制 ZIP（非 envelope）。头：`Content-Type: application/zip`、`Content-Disposition: attachment; filename="clovergo-gongzhonghao-draft-<id>.zip"`、`X-Content-Type-Options: nosniff`。内含 `manifest.json`、`draft.md`、`draft.json`。仅 `generated` 可导出，否则 409 `INVALID_STATE`。前端使用 `downloadFile(path, fallbackName)`。

#### POST `/api/v1/media-drafts/:id/wechat` 🔒

推送到公众号**草稿箱**；带查询参数 `?mode=publish` 时，进草稿箱后再调用 `freepublish/submit` 提交发布（异步发布，只返回 `publish_id`，不查询发布结果；需要公众号具备发布权限）。草稿已有 `wechat_media_id` 但还没有 `publish_id` 时，发布模式直接提交已有草稿、不再重复 `draft/add`。同一草稿同时只允许一个推送，并发请求返回 `INVALID_STATE`。同步长请求。
请求二选一：
- 空 body、不带 Content-Type：`api(path, {method:"POST"})`；
- `multipart/form-data`，可选字段 `cover`（长文封面，JPEG/PNG，≤10MB）、`image1`..`image3`（图文帖第 N 张图，JPEG/PNG/GIF，≤10MB，服务端等比缩小到 1080×1440 内并转 JPEG；未上传的位置使用只含标题的卡片（可用 `GET /api/v1/imaging/card?title=&index=&total=` 预览，返回 `{svg}`））。**不要用 `postJSON`**（会带 JSON Content-Type，multipart 读不到文件）。文件超 10MB → `FILE_TOO_LARGE`；图片无法解码 → `UNSUPPORTED_FORMAT`。

响应：`MediaDraft`（含 `wechat_media_id`、`wechat_pushed_at`；发布模式另含 `wechat_publish_id`、`wechat_published_at`）。发布失败时草稿已在草稿箱，错误信息以 “the draft was pushed to the draft box, but publishing failed” 开头并带微信 errcode，可重试。
行为：长文封面优先用上传的 `cover`，否则按 `cover_image_prompt` 本地渲染，否则默认封面 `thumb_media_id`；本机无 SVG 光栅化工具时长文丢弃配图仍可推送；图文帖存在未上传位置时直接 409 `INVALID_STATE`（全部上传则不需要光栅化工具）。
错误：`INVALID_STATE`、`WECHAT_NOT_CONFIGURED`(503)、`WECHAT_UPSTREAM_ERROR`(502)、`INVALID_INPUT`（长文无封面且无默认封面 / 代理 URL 无效）、`FILE_TOO_LARGE`(413)、`UNSUPPORTED_FORMAT`(415)。

---

## 4. 长任务与轮询速查

| 操作 | 接口 | 模式 | 如何得知完成 |
| --- | --- | --- | --- |
| 导入资料 | `POST /materials`、`/materials/text` | 同步（含一次总结调用，数秒~数十秒） | 响应即完成，只归档不提炼 |
| 提炼 | `POST /materials/:id/reprocess` | 异步，202 | 轮询 `GET /documents/:id` 的 `status`（`processing`→`published`/`processed_empty`/`analysis_failed`）；`GET /stats` 的 `processing`；失败累计 3 次转 `analysis_failed` |
| 问答 | `POST /ask` | 同步长请求 | 响应 |
| Agent | `POST /agent/chat` | 同步长请求（≤8 轮） | 响应 |
| 草稿生成 | `POST /media-drafts` | 异步，201 | 轮询 `GET /media-drafts/:id` 的 `status` |
| 向量化 | `POST /vectors/reindex` | 同步，每批 ≤200 | 看 `remaining`，>0 继续调用 |
| 推送 | `POST /media-drafts/:id/wechat` | 同步长请求 | 响应 |

UI 要求：进行中状态、可见耗时；界面不显示 `run_id`。多文件上传按顺序逐个调用，单个失败不影响其余。

---

## 5. types.ts 缺失类型（TS 建议）

```ts
export interface AuthToken { token: string }
export interface AuthPasswordResult { enabled: boolean; token?: string } // set: {enabled:true, token}; remove: {enabled:false}
export interface QueueStatus {
  pending: number; draft_pending: number; vector_pending: number; document_vector_pending: number;
}
export interface WorkflowDef { key: string; version: string }
export interface DocumentDetail { document: DocumentItem; knowledge: KnowledgeItem[] }
export interface KnowledgeUpdateInput { kind: string; title: string; body: string; tags: string[] }
export interface DeleteResult { id: number; status: "deleted" }
export interface ReadResult { id: number; read_count: number }
export interface GenerateDraftInput {
  platform: PlatformID; format?: DraftFormat; topic?: string; angle?: string; audience?: string;
  knowledge_ids?: number[]; document_ids?: number[]; random_knowledge?: boolean;
}
export interface MediaDraftBody { article?: ArticleContent; gallery?: GalleryContent }
export interface ToolSearchInput { query: string; limit?: number }
export interface ToolGetDocumentInput { document_id: number }
// 现有类型需修正：ReindexResult 增加 run_id: string；Answer 去掉后端不存在的 text?
```

---

## 6. 已知不一致

文档/代码之间发现的问题（以代码为准）：

1. AGENTS.md 称「没有空间/租户/Pack 维度」，但代码实际有 `topic`（书架）：`documents.topic`、知识的 `topics`、`GET /topics`、`PUT /documents/:id/topic`、列表/检索的 `topic`/`language` 过滤；AGENTS.md 接口部分未提。
2. AGENTS.md 把 `/health/queue` 当健康类接口；实际是受保护的 `/api/v1/health/queue`。根路径健康只有 `/health/live`、`/health/ready`。
3. `/api/v1/tools/search_sources`、`/tools/get_document`、`GET /workflows`、`auth/*`（全局密码）在 AGENTS.md 中未作为 HTTP 接口列出。
4. `internal/httpapi/materials.go` 的 `importMaterial` 注释写 “Extraction runs in the background”，与实际不符（导入只归档，不自动提炼）；`internal/service/ingest.go` 注释提到 `/documents/:id/reprocess`，实际路径是 `/materials/:id/reprocess`。
5. 后端 `NOT_FOUND`（未知路由）无前端 `error.NOT_FOUND` 文案。
6. types.ts：`ReindexResult` 缺 `run_id`；`Answer` 声明了后端不存在的 `text?`；`MaterialImportResult` 同时用于导入（201/200）与 reprocess（202）。
7. 前端 `error.WECHAT_NOT_CONFIGURED` 文案仍提示「在 .env 设置 WECHAT_APP_ID…」，而现状应在「配置 → 接口配置」填写（`.env` 只是尚未保存时的初始值）。

---

## 7. 附录：前端 `api.ts` 用法

文件：`frontend/src/lib/api.ts`。

| 导出 | 说明 |
| --- | --- |
| `ApiError(message, code, status, runID, requestID)` | 非 2xx 或网络错误抛出；网络失败 `code="NETWORK_ERROR"`、`status=0` |
| `getToken()` / `setToken(t)` | 读写 `localStorage["clovergo.token"]`（try/catch 包裹） |
| `LOCKED_EVENT` (`"clovergo:locked"`) | 收到 401 + `AUTH_REQUIRED` 时清 token 并在 `window` 派发，App 据此弹出解锁页 |
| `withQuery(path, params)` | 拼查询串；跳过 `undefined` / `null` / `""` |
| `api<T>(path, init)` | 自动带 `X-CloverGo-Token`；返回 `payload.data`；非 2xx 抛 `ApiError` |
| `postJSON<T>(path, body?)` / `putJSON<T>(path, body?)` | 带 `Content-Type: application/json`，body 默认 `{}` |
| `deleteJSON<T>(path)` | DELETE |
| `downloadFile(path, fallbackName)` | 带 token fetch → blob → 临时 `<a download>`；文件名取自 `Content-Disposition`，失败退回 `fallbackName` |
| `describeError(error)` | 返回 `{ code, title, detail, runID, requestID }`；`title` 优先取 `error.<CODE>` 的 i18n |
| `errorText` | 把错误转成可显示文本 |

示例：

```ts
// 查询
const page = await api<Paged<KnowledgeItem>>(withQuery("/api/v1/knowledge", { kind, topic, limit: 20, offset: 0 }));

// JSON 写
const draft = await postJSON<MediaDraft>("/api/v1/media-drafts", { platform: "gongzhonghao", format: "article", topic });

// 空 body 的 POST（reprocess / read / test / 推送不带封面）
await api("/api/v1/materials/5/reprocess", { method: "POST" });

// 上传：FormData，不要手设 Content-Type
const form = new FormData(); form.append("file", file); form.append("topic", topic);
await api<MaterialImportResult>("/api/v1/materials", { method: "POST", body: form });

// 推送带封面
const fd = new FormData(); fd.append("cover", coverFile);
await api<MediaDraft>(`/api/v1/media-drafts/${id}/wechat`, { method: "POST", body: fd });

// ZIP 导出
await downloadFile(`/api/v1/media-drafts/${id}/export`, `clovergo-gongzhonghao-draft-${id}.zip`);

// 错误处理
try { /* ... */ } catch (e) { const { title, detail, runID } = describeError(e); toast(title, detail, runID); }
```

### 乐观锁 `if_revision`

适用：平台风格（`PUT /platforms/:id/style`、`POST /platforms/:id/style/reset`）与提示词层（`PUT /prompts/layers/:key`、`POST /prompts/layers/:key/reset`）。

1. 先 GET 拿到当前 `revision`；
2. 保存/重置时把它作为 `if_revision` 放进请求体；
3. 成功后用响应里的新 `revision` 更新本地状态；
4. 若返回 409 `REVISION_CONFLICT`：提示他人/另一窗口已修改，重新 GET 后让用户决定是否覆盖。

```ts
try {
  const updated = await putJSON<Platform>(`/api/v1/platforms/${p.id}/style`, {
    style_rules: text, if_revision: p.revision, note,
  });
} catch (e) {
  if (e instanceof ApiError && e.code === "REVISION_CONFLICT") { /* 重新加载 */ }
}
```
