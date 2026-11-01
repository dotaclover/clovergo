# BACKEND —— 后端结构与关键行为

[AGENTS.md](../AGENTS.md) 的配套文档：改后端前先完整读本文件与真实代码。本文与 AGENTS.md 冲突时，以 AGENTS.md 为准；结构与行为以仓库实现为准。

## 1. 目录职责

| 位置 | 职责 |
| --- | --- |
| `main.go`、`internal/app` | 进程、端口、装配、恢复、内嵌 `web/dist`、优雅退出、构建版本 |
| `internal/config` | 环境配置与基准目录（安装目录有 `.env` 时以 exe 目录为准）；AI 配置只有 `ARK_*` |
| `internal/extractor` | 文本抽取（`ExtractText` 返回整篇文本；`.docx` 由 `docx.go` 转纯文本）与 `NormalizeText` 规范化 |
| `internal/prompts` | `layers/*.md` 可替换的提示词层（`//go:embed`），`core.go` 加载层并生成内容哈希版本 `CoreVersion`；`compose.go` 组装 `Summary/Extraction/Answer/Draft/Agent` 与格式约束，接受所有者覆盖 |
| `internal/platforms` | 平台默认设置（只有 `style_rules`）、格式常量、`ArticleRules()`（给界面显示的规则）、`Validate`、`Apply` |
| `internal/workflows` | 固定 Workflow 标识 |
| `internal/ark` | `client.go`（协议、错误分类、强制 tool、共用 `send`）、`summary.go`（整篇总结）、`chat.go`（Agent 自由工具调用）、`extract.go`（整篇提炼）、`answer.go`、`media.go`（文章 schema 与校验）、`embed.go` |
| `internal/wechat` | 公众号草稿箱客户端（stable_token、上传图片、news/newspic 的 draft/add、可选 freepublish/submit）与可选 SOCKS5 代理拨号 |
| `internal/imaging` | 标题 → SVG 卡片 → PNG 的本地光栅化（无图片模型） |
| `internal/domain` | `document.go`、`knowledge.go`、`run.go`、`media.go`、`wechat.go`、`agent.go`、`lock.go` |
| `internal/service` | `service.go`、`settings.go`（平台风格、Prompt 查看）、`ingest.go`（整篇总结 + 提炼）、`roots.go`（文件夹登记、扫描、单文件导入）、`queue.go`、`knowledge.go`（含已读计数、分类合并）、`retrieval.go`、`vectors.go`（手动向量化）、`ask.go`、`agent.go`（Agent 循环与工具）、`media.go`（异步生成、编辑、删除）、`export.go`、`wechat.go`（接口配置、推送与 HTML 渲染）、`run.go`、`lock.go`（全局密码与令牌） |
| `internal/repository` | `sqlite.go`（打开、迁移、FTS、恢复）、`documents.go`、`roots.go`、`knowledge.go`（检索、版本、使用次数）、`platforms.go`、`vectors.go`、`runs.go`、`media.go`、`wechat.go`、`stats.go`、`lock.go`、`repository.go` |
| `internal/httpapi` | `router.go`、`guard.go`、`response.go`、`upload.go`、`materials.go`、`roots.go`、`knowledge.go`、`ask.go`、`agent.go`（Agent 与推送）、`media.go`、`settings.go`、`wechat.go`、`runs.go`、`lock.go`（`/auth/*` 与 `requireUnlock`） |
| `internal/tools` | 只读 `SearchSources`（仅关键词，`POST /tools/search_sources`）；`get_document` 的实现在 `service.GetSourceDocument` |
| `scripts` | `build.ps1`、`install.ps1`、`doctor.ps1` |

分层：HTTP 只解析与响应；状态转换在 service；持久化在 repository；厂商 JSON 与输出校验在 ark。新增平台格式需要同时改：`platforms`（默认、范围与格式常量）、`prompts.FormatRules`、`ark/media.go`（schema 与校验）、`service/export.go`（Markdown 渲染）、前端渲染器，并做真实验收。

## 2. 资料与知识是两张表

`documents`（原文件存磁盘 `data/library/<sha256><ext>`，表里只存路径 `stored_path` 与整篇 `summary`，不存正文）与 `knowledge_items`（证据内联：`evidence_quote` 与 `quote_start/quote_end`）。没有 `chunks`/`candidates`/`knowledge_evidence`。上传按上限（`MAX_DOCUMENT_RUNES`，默认 200000 rune）整篇一次方舟总结调用；提炼把整篇文本在**单次方舟调用**中变成 N 条知识——不做分窗、不逐 chunk 循环。**故意不做跨资料合并**：一条知识只有一份来源与一处证据。

## 3. 资料文件夹扫描导入

`service/roots.go`、`httpapi/roots.go`、表 `library_roots`。所有者在界面点「选择文件夹」，后端在本机弹系统对话框（`POST /pick-folder`，仅 Windows，只返回路径，空串=取消；服务只监听本机）后自动登记并扫描（路径是所有者档案，不是不可信数据）；`POST /roots/:id/scan` 只读遍历，不调用模型、不导入，返回新增/变更/跳过/已消失/未变化/库中已有的报告；所有者勾选后前端逐个调 `POST /roots/:id/import`（每个文件一次总结，单个失败不影响其余）。

**信任边界**：只处理 `.md/.markdown/.txt/.docx`；不跟随符号链接（`Lstat` 拒符号链接与非常规文件，`EvalSymlinks` 后 `filepath.Rel` 必须仍在根内）；导入的 `path` 必须是 `filepath.IsLocal` 的相对路径；跳过 `.`/`~$` 开头项与 `node_modules`，最多 5000 个文件。大小策略：原文件超 `MAX_UPLOAD_MB` 直接跳过，转换成纯文本后超 `MAX_DOCUMENT_RUNES` 放弃（不截断），原因列在报告里。根下文件 `source_key = root<ID>/<小写相对路径>`，变更后成为同一来源的新版本；磁盘上消失的资料置 `documents.missing`（保留，不删）。docx 抽取转纯文本，快照以 `.txt` 存盘且 sha256 基于转换后文本，保证 Evidence 偏移可复算。**Agent 不接受任意磁盘路径**，仍只能读库内资料；不要给 Agent 加文件系统工具。

## 4. 固定 Workflow 与 Agent

固定 Workflow：`knowledge.ingest`、`knowledge.answer`、`media.draft`、`agent.chat`。**Agent** 是有上限的工具调用循环（`service/agent.go`）：最多 8 轮模型调用，模型只能在代码定义的**只读**工具（`search_knowledge`（可选 `topic` 限定主题）、`get_knowledge`、`get_document`、`list_documents`、`library_overview`）中选择；回复引用了未返回过的 `[K<id>]` 时，服务端告知允许的 ID 并**重试一次**（仍然违规则整次失败，校验始终在代码里）；对话由客户端保存并每轮发回，服务端不保存会话；但每次成功回答会写入 `agent_qa_history`，供 `GET /agent/history` 分页回看。没有任意 Shell/URL/路径/写库工具；新增工具必须只读，或先在 AGENTS.md 写明信任边界。**没有选题工作流**：写什么由「创作灵感（一段自由文本，可写主题/角度/受众）或创作来源（指定知识点、指定资料原文、随机知识点）至少一项」决定。

## 5. 推送到公众号草稿箱

`internal/wechat`，`POST /media-drafts/:id/wechat`：`stable_token` → 上传图片为永久素材 → `draft/add`，默认只进草稿箱；仅当所有者在推送时显式选择「推送并发布」（`POST /media-drafts/:id/wechat?mode=publish`）才在草稿入箱后再调 `freepublish/submit`（异步发布，只记 `publish_id` 与时间到 `media_drafts.wechat_publish_id/wechat_published_at`，**不查询发布结果**；账号无发布权限时原样返回微信 errcode，错误信息注明草稿已在草稿箱、可重试，不兜底）。Agent 与任何自动流程不得触发发布。长文走 `news`（封面为 `thumb_media_id`，章节配图用 `media/uploadimg` 换成 URL 后内联）；图文帖走 `newspic`（`image_info.image_list[].image_media_id`，最多 20 张，第一张即封面，不需要额外封面）。AppID/AppSecret 在「配置 → 接口配置」里填写并存进 `wechat_settings`，环境变量（`WECHAT_*`，含 `WECHAT_PROXY_URL`）只作为尚未保存时的初始值；`POST /wechat/settings/test` 用 `stable_token` 检查连通性。access_token 只在内存，日志与错误信息不得包含 secret、token 或请求 URL。成功路径需要操作者自己的公众号验收。其余未实现能力见 README「明确未实现 / Not implemented」，任何代码、文档、UI 文案都不得把它们写成已支持。

## 6. 图片在推送时本地生成

`internal/imaging`，不请求图片模型：草稿里的每张图只是提示词。推送时才把**标题**排成 1080×1440 的 SVG 卡片（只有标题，不印提示词；提示词留给将来的图片生成接口；草稿详情页的图片位用 `GET /imaging/card` 预览同一张卡片），再用本机找到的第一个光栅化工具转成 PNG：ImageMagick（`magick`，含未加入 PATH 的 `C:\Program Files\ImageMagick*`）、`rsvg-convert`、Inkscape，最后是**确实带 SVG 解码器**的 ffmpeg（`ffmpeg -decoders` 里有 `svg`；本机常见构建没有）。有工具时：长文按 `cover_image_prompt` 生成封面（用户已上传封面则优先用户封面），章节配图上传后内联；图文帖逐张生成并上传；每张图也可由所有者在推送时上传（multipart `image1..image3`，`imaging.FitJPEG`：JPEG/PNG/GIF 解码、等比只缩小到 1080×1440 内、透明铺白、重编码 JPEG，不处理 EXIF 方向，不支持 webp），上传位不走栅格化。**没有工具时**：长文直接丢掉配图与提示词、不保留占位文字，仍可推送；图文帖在存在未上传位时返回 `INVALID_STATE`（全部上传则无需工具），因为图片就是内容本身。生成只在推送时发生，不在生成草稿时发生；图片不落盘、不进入导出包。

## 7. 推送可走 SOCKS5 代理

`wechat_settings.proxy_url`：本机公网 IP 会变，而公众号接口强制要求调用方 IP 在白名单里（空名单等于全部拒绝，没有"允许所有"；`stable_token` 同样受限）。配置 `socks5://user:pass@host:port` 后，`internal/wechat` 的 socket 经代理拨出，白名单只需填代理出口 IP；只影响公众号调用，`internal/ark` 仍直连。代理 URL 含密码，属本机配置，不得写入日志、错误信息或导出包。设置响应把用户名和密码替换为 `redacted`；原样回传遮蔽地址保留原值，空串清除，新地址替换。

## 8. 草稿不记录引用，使用计数唯一

生成草稿时知识/资料只作为写作输入，文章里不再有 `source_knowledge_ids`/`source_document_ids`，也不写 `artifact_knowledge`；本地知识无法在公众号文章里被引用，所以这套机制整体删除，不要恢复。草稿可在详情页修改（`PUT /media-drafts/:id`，与模型输出同一套文章校验）、删除（`DELETE /media-drafts/:id`），列表与详情分开两个页面。草稿成功生成时，被用作来源的知识点与资料各 `use_count + 1`，知识卡与资料行显示「已用于 N 篇」；这是唯一的使用计数，不恢复 `artifact_knowledge` 来源记录。
