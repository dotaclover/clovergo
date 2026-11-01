# CloverGo

English | [简体中文](README.md)

Current version: **0.2.0** · MIT · Experimental

**Turn personal materials into knowledge with traceable sources, then reuse it for Q&A and WeChat drafts.**

CloverGo turns the material you keep reading (notes, chat exports, articles) into small, reusable knowledge items, each tied to a verbatim quote from its source. On top of that library it runs:

- a **read-only agent** that searches and reads the library, with validation of knowledge citation IDs;
- **grounded Q&A** with citations;
- **WeChat Official Account drafts** — long-form articles and picture posts ("小绿书") — which can be pushed straight into the account's draft box.

It is one Go binary, one SQLite file and one folder of source files, with the React UI embedded. Only loopback bindings are allowed, defaulting to `127.0.0.1:8088`. No Node runtime, Docker or separate database server is needed. The UI is in Chinese by default and switches to English.

An experimental tool for individual creators and developers who want to reuse notes, articles and chat exports. Bring your own Volcano Engine Ark account; model calls incur charges on that account. **Local storage does not mean offline AI: summaries, extraction, Q&A and writing send relevant content to Ark.**

> **Built with AI.** CloverGo is an AI-coding project: its design, code, docs and reviews were written with **TraeWork**, **Claude Opus** and **GPT** as pair programmers, under the constraints in [AGENTS.md](AGENTS.md) — the core rulebook for humans and AI agents on this repo, with details in [docs/BACKEND.md](docs/BACKEND.md) and [docs/FRONTEND.md](docs/FRONTEND.md).

```text
materials (.md/.txt/.docx, pasted text, or a scanned folder)
  → summarized and archived only; nothing is extracted automatically
  → explicit "Extract" from the materials list → one-call extraction → evidence must be a verbatim, re-computable quote
  → confirmed knowledge (versions, read/usage counts, vectors)
  → optional manual embedding of material summaries and knowledge title+body
  → retrieve: keyword FTS + semantic vectors, fused with RRF
       ├─ Agent        bounded tool-calling loop, read-only tools, cited replies
       ├─ Ask          one-shot grounded answer
       └─ WeChat draft: long article, or picture post (written from knowledge or a material's original text)
            → copy / ZIP / push to the draft box; empty picture slots become local title-card images at push time
  → every model call is a Run: steps, latency, tokens, prompt version
```

## Status

| Capability | Status | Notes |
| --- | --- | --- |
| Folder scan import | ✅ tested | Register a local folder, scan it (read-only, symlinks not followed) and get a report: new / changed / unchanged / already in the library / missing / skipped files with the reason (over `MAX_UPLOAD_MB`, over `MAX_DOCUMENT_RUNES` after conversion, empty, not UTF-8, bad docx). Only `.md/.markdown/.txt/.docx` are considered; `.docx` is converted to plain text and that text is what is stored. You pick the files, then each is imported (summarized) one by one; a changed file becomes a new version of the same source |
| Import & extraction | ✅ tested | UTF-8 `.md/.markdown/.txt` and `.docx`, batch or pasted; importing only summarizes and archives the material — a material is not necessarily knowledge, it may just be an archive. Extraction is an explicit action in the materials list (one call over the whole text); a draft can also be written straight from a material's original text; re-importing a file creates a new version and retires the old evidence |
| Evidence check | ✅ tested | Every item quotes a contiguous span of its source with recomputable offsets; extraction kinds come from a fixed taxonomy, though an operator may type a custom kind when editing an item |
| Hybrid retrieval | ✅ tested, quality limitations | Keyword FTS5 + `doubao-embedding-vision` vectors over knowledge *and* materials, fused with RRF; material matches lift their extracted knowledge; items without current vectors contribute keyword hits only. Chinese queries matched in the latest check, but two English queries did not; cross-language recall is not guaranteed |
| Agent | ✅ tested | Up to 8 model turns; tools `search_knowledge` (optional `topic` filter), `get_knowledge`, `get_document`, `list_documents`, `library_overview`; `[K<id>]` citations are checked against what the tools actually returned (one correction retry, then fail); multi-turn; replies in the user's language |
| Grounded Q&A | ✅ tested | Uses retrieved knowledge as context and returns source citations; citation checks do not guarantee every claim is correct |
| WeChat article drafts | ✅ tested | Lead + 2–5 sections + conclusion, title, summary, cover image prompt, tags; about 1000 characters of prose, never more than 2000; written from an inspiration alone, a named knowledge item, a material's original text, or a random knowledge item. Sources are input only and are not cited in the article. Generation runs in the background: the request returns at once with `status: running`, an invalid model response is retried once, and the list updates itself when the article is ready. Every finished article is also written to `data/drafts/<id>-<title>.md` as copy-ready Markdown, and can be opened on its own page to edit, delete or push |
| WeChat picture posts | ✅ tested | A short plain-text caption plus 1–3 images (default 1). Each image has a prompt; at push time you can upload your own picture per slot (JPEG/PNG/GIF, automatically fitted within 1080×1440 and re-encoded as JPEG), otherwise the post title is laid out as an SVG card and rasterized locally (ImageMagick, librsvg, Inkscape, or an ffmpeg build with an SVG decoder). No image model is called. The post is pushed as `article_type: newspic`; the first image is the cover. If a slot has no upload and the machine has no SVG rasterizer the push is refused |
| Push to WeChat draft box | ⚠️ implemented, partly verified | Long articles: `stable_token` → cover upload → `draft/add`; picture posts: render + upload each card → `draft/add` with `newspic`. A successful push needs your own account credentials, filled in under **Configuration → Credentials**. An optional SOCKS5 proxy in the same tab gives a local install a fixed egress IP, so the account's IP allowlist does not need changing whenever the home IP changes |
| Bilingual UI | ✅ | Chinese default, English switch, stored in `localStorage` |
| Local-only guard | ✅ tested | Rejects foreign Host, cross-site requests, non-JSON writes |
| Global password (lock screen) | ✅ tested | Optional, set under Settings → Security. Stored as a salted PBKDF2 hash in SQLite (`app_lock`); the browser keeps an unlock token in `localStorage`. The server enforces it on every `/api/v1/*` call except the public `auth/status` and `auth/unlock`; changing or removing the password, or restarting the server, invalidates old tokens (they also expire after 30 days). A local lock, not user accounts — anyone who can read the data folder can still read the data |

“Tested” includes historical checks; see the latest [release verification record](docs/RELEASE-CHECK.md) for scope and limits. Historical samples from 2026-10-18: an agent question took 2 searches and 9.4 s; a follow-up used 4 model turns, 6 tool calls and 18.3k tokens; an off-topic question answered that the library did not cover it. These samples are not performance, cost or accuracy guarantees.

**Not implemented**: scheduled or automatic publishing, publishing to other platforms, publish-status queries, AI image generation (pictures are local title cards), web search, PDF/OCR, legacy DOC and other Office formats such as Excel/PPT, accounts or multi-tenancy, LAN/public deployment, or agent tools that write data, run commands or open URLs. DOCX support extracts text only, without layout or embedded images.

## Data and privacy

- Source snapshots, knowledge, drafts, run records and answered questions are stored locally. Run records can contain inputs and model outputs, not just timing/token metadata. The app lock does not encrypt files or SQLite.
- Summarization and extraction send the whole document to Ark. Q&A, the agent and writing send questions, relevant conversation and selected source/retrieval context. Optional embeddings send material summaries, knowledge text and search queries.
- WeChat credentials, drafts and images are sent to WeChat only when its integration is used. Explicitly submitting a publish task does not confirm successful publication.
- Ark keys come from the environment or ignored `.env`. WeChat credentials and proxy settings are stored in local SQLite. Settings responses omit AppSecret and mask proxy usernames/passwords. Saving the unchanged masked URL preserves credentials; clearing it removes the proxy; a new URL replaces it.
- Share source or built binaries, excluding `.env`, `data/`, `.local/`, databases, logs, browser storage and private screenshots.

Evidence checks establish that a quoted span occurs in the source. Agent citation checks establish that an ID was returned by a tool in that run. Neither proves the source true nor eliminates hallucinations; review generated content before use.

## Quick start

Requires Go 1.26+. Node 22+ is only needed to rebuild the UI.

```powershell
Copy-Item .env.example .env
notepad .env        # set ARK_API_KEY and an ARK_MODEL enabled for your account
go run .            # http://127.0.0.1:8088
```

| Variable | Purpose |
| --- | --- |
| `ARK_API_KEY`, `ARK_MODEL`, `ARK_BASE_URL` | Volcano Engine Ark chat model (official `volces.com` HTTPS only) |
| `ARK_EMBEDDING_MODEL`, `ARK_EMBEDDING_DIMENSIONS` | Optional semantic search; empty means keyword-only |
| `AUTO_PUBLISH_MIN_CONFIDENCE` | Extracted knowledge the model scores below this confidence (default `0.72`) is dropped |
| `WECHAT_APP_ID`, `WECHAT_APP_SECRET` | Optional seed for pushing drafts to the Official Account draft box; once saved on the settings screen the stored values win. The address the push leaves from — this machine's public IP, or the proxy's egress IP when one is configured — must be on the account's API IP allowlist |
| `WECHAT_AUTHOR`, `WECHAT_THUMB_MEDIA_ID` | Optional seed author name and default cover `media_id` |
| `WECHAT_PROXY_URL` | Optional seed `socks5://user:password@host:port` proxy for pushes |

Ark keys are read only from the process environment or the git-ignored `.env`; they never reach SQLite, logs, exports or the UI. Official Account credentials are entered by the owner under **Configuration → Credentials** and stored in the local SQLite (`wechat_settings`); they are never logged or exported. The same tab accepts an optional `socks5://user:password@host:port` proxy: pushes then leave from the proxy's address, which is the one to put on the account's IP allowlist (the allowlist cannot be left open, and `stable_token` is bound by it too). Without an Ark key, model features fail with `ARK_NOT_CONFIGURED` instead of producing fake output.

Pushing lands a draft in the account's **draft box**; only when you choose "Push and publish" (`?mode=publish`) is it then submitted via `freepublish/submit` (needs the account's publish permission; the result of the asynchronous publish is not queried). Local title cards are rendered at push time; without an SVG-to-PNG converter a long article omits these images. A picture post requires either an uploaded image in every slot or a converter for the remaining slots. Check the images before publishing.

Frontend development:

```powershell
Set-Location frontend
npm ci
npm run dev         # 127.0.0.1:5173, proxies /api to 8088
npm run build       # type-check and write ../web/dist (committed, embedded in the binary)
```

## The agent

`POST /api/v1/agent/chat` with `{"messages":[{"role":"user","content":"..."}]}`. The conversation lives in the client and is sent back in full each turn (max 20 turns); the server keeps no session, but it does log each answered question so `GET /api/v1/agent/history` can list it.

- The model chooses among a fixed set of read-only tools; the service runs them through the same paths as the UI.
- Tool results are treated as data, never as instructions.
- A reply that cites an ID no tool returned in this run is rejected (`ARK_INVALID_RESPONSE`); running out of turns returns `AGENT_STEP_LIMIT`.
- Every turn and tool call is recorded as a Run event on the server (the UI no longer shows Runs).

The direction is a **local agent**: today the loop calls Ark over an OpenAI-compatible chat API, and the loop, tools and checks are independent of the vendor.

## API

Writes must be `application/json`, except multipart uploads. Responses are `{"data", "request_id"}` or `{"error": {"code", "message"}, "request_id", "run_id"?}`. The list endpoints `/api/v1/knowledge`, `/api/v1/media-drafts` and `/api/v1/agent/history` accept `?limit&offset` and return `{"items", "total"}` so the UI can page them.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health/live`, `/health/ready` | Liveness / DB, FTS, model, embedding and WeChat configuration |
| `POST` | `/api/v1/materials`, `/materials/text` | Upload a file / paste text (archives only; extraction is manual) |
| `POST` | `/api/v1/materials/:id/reprocess` | Extract knowledge from one material (runs in the background) |
| `GET/POST` | `/api/v1/roots` | Registered library folders / register one (`{"path","name"}`) |
| `DELETE` | `/api/v1/roots/:id` | Unregister a folder (imported materials stay) |
| `POST` | `/api/v1/pick-folder` | Open the native folder picker on this machine (Windows only); returns `{"path"}`, empty = cancelled |
| `POST` | `/api/v1/roots/:id/scan` | Scan a folder and return the report (`new, changed, skipped, missing, unchanged, known, other, truncated, error_dirs`); `known` is always 0, files under unreadable `error_dirs` are not marked missing |
| `POST` | `/api/v1/roots/:id/import` | Import one scanned file (`{"path","topic"}`, `path` relative to the folder) |
| `GET` | `/api/v1/documents`, `/documents/:id` | Materials and extraction progress |
| `GET` | `/api/v1/documents/:id/text` | One material's stored original text, re-read from disk |
| `PUT`, `DELETE` | `/api/v1/documents/:id` | Rename (`{"filename"}`) / delete a material with its knowledge and vectors (rejected while extracting) |
| `PUT` | `/api/v1/documents/:id/topic` | File a material under a topic (`{"topic"}`; empty = unfiled) |
| `GET` | `/api/v1/topics` | Topics in use |
| `GET` | `/api/v1/knowledge`, `/knowledge/:id` | Knowledge list (paged; filters `q`, `kind`, `topic`, `language`, `usage=used\|unused`) / one item; items carry `use_count` and `read_count` |
| `PUT/DELETE` | `/api/v1/knowledge/:id` | Edit an item (title, body, kind, tags) / delete it |
| `POST` | `/api/v1/knowledge/:id/read` | Count one read of an item |
| `GET` | `/api/v1/search?q=` | Hybrid retrieval |
| `POST` | `/api/v1/ask` | Grounded Q&A |
| `POST` | `/api/v1/agent/chat` | Agent turn |
| `GET` | `/api/v1/agent/history` | Answered questions, paged |
| `POST/GET` | `/api/v1/media-drafts` | Queue a draft (`format`: `article` or `gallery`; returns `status: running`) / list drafts (list is paged) |
| `GET/PUT/DELETE` | `/api/v1/media-drafts/:id` | One draft; `PUT` saves an owner edit (`{"article": …}` or `{"gallery": …}`, same checks as model output), `DELETE` removes it |
| `GET` | `/api/v1/media-drafts/:id/export` | ZIP export |
| `POST` | `/api/v1/media-drafts/:id/wechat` | Push to the draft box; empty body or multipart with `cover` (articles) / `image1..image3` (picture posts); `?mode=publish` also submits it for publishing. One push per draft at a time |
| `GET` | `/api/v1/imaging/card?title=&index=&total=` | Preview the local title card used for image slots without an upload (`{svg}`) |
| `GET/PUT` | `/api/v1/wechat/settings` | Official Account AppID/AppSecret, author, default cover and optional SOCKS5 proxy, stored locally. The secret is never returned (`app_secret` is `""`, `app_secret_set` tells whether one is saved); an empty secret on `PUT` keeps the saved one |
| `POST` | `/api/v1/wechat/settings/test` | Fetch an access token to verify the saved credentials |
| `GET/PUT` | `/api/v1/platforms`, `/platforms/:id/style` | Style rules (with `if_revision`) |
| `POST` | `/api/v1/platforms/:id/style/reset` | Restore the built-in style rules |
| `GET` | `/api/v1/prompts` | Every composed system prompt, layer by layer |
| `GET/PUT/POST` | `/api/v1/prompts/layers[/:key[/reset]]` | Embedded Markdown prompt layers; the owner replaces one layer (with `if_revision`) or restores the built-in text |
| `GET` | `/api/v1/stats`, `/api/v1/health/queue` | Library counts and pending work (knowledge + material vectors) |
| `GET` | `/api/v1/kinds` | Knowledge taxonomy: built-in categories plus custom kinds already in use |
| `POST` | `/api/v1/vectors/reindex` | Embed pending knowledge and materials (optional `{"limit"}`) |
| `GET` | `/api/v1/runs`, `/runs/:run_id` | Recent runs / one run with its events |
| `GET` | `/api/v1/workflows` | The fixed workflows and their versions |
| `POST` | `/api/v1/tools/search_sources`, `/tools/get_document` | Read-only tools: keyword search over knowledge / one material's text |
| `GET/POST` | `/api/v1/auth/status`, `/auth/unlock` | Global password lock (public): `{enabled, unlocked}` / `{password}` → `{token}` |
| `PUT/POST` | `/api/v1/auth/password`, `/auth/password/remove` | Set or change the password (`{current, password}` → `{token}`) / remove it (`{current}`). Once a password is set, every other `/api/v1/*` call needs the `X-CloverGo-Token` header (valid 30 days; a restart, change or removal of the password invalidates it); only `/health/live`, `/health/ready` and the static UI stay open |

| Error code | Meaning |
| --- | --- |
| `INVALID_INPUT`, `TOOL_INPUT_INVALID` | Malformed request or tool arguments |
| `UNSUPPORTED_FORMAT`, `FILE_TOO_LARGE`, `DOCUMENT_EMPTY` | The upload is not UTF-8 `.md/.markdown/.txt` or a valid `.docx`, is over the size limit, or has no text |
| `ARK_NOT_CONFIGURED`, `ARK_AUTH_FAILED`, `ARK_RATE_LIMITED`, `ARK_TIMEOUT`, `ARK_UPSTREAM_ERROR`, `ARK_INVALID_RESPONSE` | Model configuration or call failures |
| `ROOT_NOT_FOUND` | The registered folder does not exist |
| `EMBEDDING_NOT_CONFIGURED` | Vector operation without an embedding model |
| `NO_GROUNDED_KNOWLEDGE` | No usable confirmed knowledge |
| `AGENT_STEP_LIMIT` | The agent did not finish within its turn limit |
| `WECHAT_NOT_CONFIGURED` | AppID / AppSecret not filled in under Configuration → Credentials |
| `WECHAT_UPSTREAM_ERROR` | WeChat API error (`errcode` / `errmsg` included) |
| `AUTH_REQUIRED` | A global password is set and the request carries no valid `X-CloverGo-Token` |
| `PASSWORD_INCORRECT` | Wrong password (unlock, change or remove) |
| `TOO_MANY_ATTEMPTS` | 5 wrong passwords in a row; locked for 30 seconds (counter is in memory) |
| `REVISION_CONFLICT` | Style rules changed elsewhere |
| `DOCUMENT_NOT_FOUND`, `KNOWLEDGE_NOT_FOUND`, `MEDIA_DRAFT_NOT_FOUND`, `RUN_NOT_FOUND`, `SOURCE_NOT_FOUND` | Missing record |
| `INVALID_STATE` | The record is in the wrong state: a draft still running or failed cannot be edited, exported or pushed; a material already extracting cannot be extracted again; a picture post cannot be pushed without an SVG rasterizer unless every image slot is uploaded |
| `RUN_INTERRUPTED` | The process stopped while this run was in progress |
| `FORBIDDEN_ORIGIN`, `UNSUPPORTED_MEDIA_TYPE` | Rejected by the local guard |
| `INTERNAL_ERROR` | Unexpected server failure (details only in the log) |

## Layout

```text
main.go, internal/app   process, wiring, embedded UI
internal/config         environment configuration
internal/extractor      text extraction and normalization
internal/prompts        embedded Markdown prompt layers and composition
internal/ark            Ark chat (forced tool + free tool calling), embeddings, output checks
internal/service        business logic: ingest, retrieval, ask, agent, drafts, WeChat push
internal/wechat         Official Account draft-box client (api.weixin.qq.com only)
internal/imaging        title → SVG card → PNG via a local converter (no image model)
internal/repository     SQLite, migrations, FTS, vectors
internal/httpapi        routes, local guard, error envelope
frontend/, web/dist     React + Vite source and the committed build
scripts/                build / install / doctor (Windows PowerShell)
```

## License

[MIT](LICENSE)
