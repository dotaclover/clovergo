package httpapi

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *service.Service
	log     *slog.Logger
}

type Options struct {
	ListenAddress string
	Version       string
}

func NewRouter(svc *service.Service, log *slog.Logger, webFS fs.FS, options Options) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler := &Handler{service: svc, log: log}
	router.Use(requestID(), accessLog(log), recovery(log), localOnly(log, options.ListenAddress))

	router.GET("/health/live", func(c *gin.Context) {
		respond(c, http.StatusOK, gin.H{"status": "live"})
	})
	router.GET("/health/ready", func(c *gin.Context) {
		if err := svc.Ready(c.Request.Context()); err != nil {
			respondError(c, log, &service.AppError{Code: service.CodeInternal, Message: "database is not ready", Cause: err})
			return
		}
		wechatConfigured, wechatDefaultCover := svc.WechatStatus(c.Request.Context())
		respond(c, http.StatusOK, gin.H{
			"status": "ready", "fts_mode": svc.FTSMode(),
			"ark_configured": svc.ArkConfigured(), "ark_model": svc.ArkModel(), "version": options.Version,
			"embedding_configured": svc.EmbeddingConfigured(), "embedding_model": svc.EmbeddingModel(),
			"wechat_configured": wechatConfigured, "wechat_default_cover": wechatDefaultCover,
		})
	})

	// The lock screen talks to these two before it has a token.
	public := router.Group("/api/v1", requireJSON(log))
	public.GET("/auth/status", handler.authStatus)
	public.POST("/auth/unlock", handler.unlock)

	api := router.Group("/api/v1", requireJSON(log), handler.requireUnlock())
	{
		// Global password: set/change and remove need the current token.
		api.PUT("/auth/password", handler.setPassword)
		api.POST("/auth/password/remove", handler.removePassword)

		// Registries.

		// Owner settings: platform styles, prompts, vectors. Extraction kinds
		// are fixed in code; an operator may add a custom kind by editing an
		// item, and /kinds reports both.
		api.GET("/kinds", handler.listKinds)
		api.GET("/platforms", handler.listPlatforms)
		api.PUT("/platforms/:id/style", handler.updatePlatformStyle)
		api.POST("/platforms/:id/style/reset", handler.resetPlatformStyle)
		api.GET("/imaging/card", handler.cardPreview)
		api.GET("/prompts", handler.listPrompts)
		// The embedded Markdown prompt layers, replaceable by the owner.
		api.GET("/prompts/layers", handler.listPromptLayers)
		api.PUT("/prompts/layers/:key", handler.savePromptLayer)
		api.POST("/prompts/layers/:key/reset", handler.resetPromptLayer)
		api.POST("/vectors/reindex", handler.reindexVectors)
		api.GET("/health/queue", handler.queueStatus)
		api.GET("/stats", handler.libraryStats)
		api.GET("/workflows", func(c *gin.Context) { respond(c, http.StatusOK, svc.ListWorkflows()) })

		// Knowledge base: materials, documents, knowledge, and review.
		api.POST("/materials", handler.importMaterial)
		api.POST("/materials/text", handler.importText)
		api.POST("/materials/:id/reprocess", handler.reprocessMaterial)
		// Folders the owner registered: scan reports, then per-file import.
		api.GET("/roots", handler.listRoots)
		api.POST("/pick-folder", handler.pickFolder)
		api.POST("/roots", handler.addRoot)
		api.DELETE("/roots/:id", handler.deleteRoot)
		api.POST("/roots/:id/scan", handler.scanRoot)
		api.POST("/roots/:id/import", handler.importRootFile)
		api.GET("/documents", handler.listDocuments)
		api.GET("/documents/:id", handler.getDocument)
		// The stored text of one material, so the materials list can show the
		// original document behind a citation.
		api.GET("/documents/:id/text", handler.getDocumentText)
		api.PUT("/documents/:id/topic", handler.updateDocumentTopic)
		api.PUT("/documents/:id", handler.updateDocument)
		api.DELETE("/documents/:id", handler.deleteDocument)
		api.GET("/knowledge", handler.listKnowledge)
		api.GET("/knowledge/:id", handler.getKnowledge)
		api.PUT("/knowledge/:id", handler.updateKnowledge)
		api.DELETE("/knowledge/:id", handler.deleteKnowledge)
		api.POST("/knowledge/:id/read", handler.markKnowledgeRead)
		api.GET("/topics", handler.listTopics)
		api.GET("/search", handler.search)

		// Services driven by the knowledge base.
		api.POST("/ask", handler.ask)
		api.POST("/media-drafts", handler.generateDraft)
		api.GET("/media-drafts", handler.listDrafts)
		api.GET("/media-drafts/:id", handler.getDraft)
		api.PUT("/media-drafts/:id", handler.updateDraft)
		api.DELETE("/media-drafts/:id", handler.deleteDraft)
		api.GET("/media-drafts/:id/export", handler.exportDraft)
		// Copies a draft into the draft box; mode=publish explicitly submits it.
		api.POST("/media-drafts/:id/wechat", handler.pushDraftToWechat)
		// The Official Account configuration, changed from the settings screen.
		api.GET("/wechat/settings", handler.getWechatSettings)
		api.PUT("/wechat/settings", handler.saveWechatSettings)
		api.POST("/wechat/settings/test", handler.testWechatSettings)

		// The agent: a bounded, read-only tool-calling loop over the library.
		api.POST("/agent/chat", handler.agentChat)
		api.GET("/agent/history", handler.listAgentQA)

		// Restricted local tools and run traces.
		api.POST("/tools/search_sources", handler.searchSources)
		api.POST("/tools/get_document", handler.getSourceDocument)
		api.GET("/runs", handler.listAgentRuns)
		api.GET("/runs/:run_id", handler.getAgentRun)
	}

	assets, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		return nil, err
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, fmt.Errorf("embedded web build is missing index.html (run npm run build in frontend): %w", err)
	}
	router.GET("/", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
	assetServer := http.FileServerFS(assets)
	router.GET("/assets/*filepath", func(c *gin.Context) {
		if strings.HasSuffix(c.Param("filepath"), "/") {
			c.Status(http.StatusNotFound)
			return
		}
		// Vite emits content-hashed file names, so assets can be cached indefinitely.
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		assetServer.ServeHTTP(c.Writer, c.Request)
	})

	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, errorEnvelope{
			Error: errorBody{Code: "NOT_FOUND", Message: "route not found"}, RequestID: c.GetString(requestIDKey),
		})
	})
	return router, nil
}
