package httpapi

import (
	"net/http"
	"strconv"

	"clovergo/internal/domain"
	"clovergo/internal/imaging"
	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *Handler) generateDraft(c *gin.Context) {
	var request service.GenerateDraftInput
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "platform is required; inspiration (topic, angle, audience), a source (knowledge_ids, document_ids, random_knowledge) or both are needed", Cause: err})
		return
	}
	draft, err := h.service.GenerateDraft(c.Request.Context(), request)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusCreated, draft)
}

// cardPreview returns the title card that is pushed when a slot has no upload,
// as SVG text, so the draft page can show it before pushing.
func (h *Handler) cardPreview(c *gin.Context) {
	index, _ := strconv.Atoi(c.DefaultQuery("index", "1"))
	total, _ := strconv.Atoi(c.DefaultQuery("total", "1"))
	title := []rune(c.Query("title"))
	if len(title) > 200 {
		title = title[:200]
	}
	respond(c, http.StatusOK, gin.H{"svg": string(imaging.Card(string(title), max(index, 1), max(total, 1)))})
}

func (h *Handler) listDrafts(c *gin.Context) {
	drafts, total, err := h.service.ListDrafts(c.Request.Context(), c.Query("platform"), queryLimit(c), queryOffset(c))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"items": drafts, "total": total})
}

func (h *Handler) getDraft(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	draft, err := h.service.GetDraft(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, draft)
}

func (h *Handler) exportDraft(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	data, filename, err := h.service.ExportDraft(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respondZIP(c, data, filename)
}

// updateDraft saves an owner edit of a generated draft. The body carries either
// an article or a picture post, and the service checks it like model output.
func (h *Handler) updateDraft(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	var body domain.MediaDraftBody
	if err := decodeStrictJSON(c, &body); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "a complete article or picture post is required", Cause: err})
		return
	}
	draft, err := h.service.UpdateDraft(c.Request.Context(), id, body)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, draft)
}

func (h *Handler) deleteDraft(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	if err := h.service.DeleteDraft(c.Request.Context(), id); err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"id": id, "status": "deleted"})
}
