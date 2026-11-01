package httpapi

import (
	"net/http"

	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

// listKinds returns the knowledge taxonomy so the UI can label and filter
// knowledge without hard-coding the ids. Custom kinds already in use are
// included, so a category an operator typed can be picked again.
func (h *Handler) listKinds(c *gin.Context) {
	kinds, err := h.service.Kinds(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, kinds)
}

// queueStatus reports how many materials are waiting for background
// extraction and how many knowledge items and materials still lack a vector, so
// the UI can show progress without polling each document.
func (h *Handler) queueStatus(c *gin.Context) {
	vectorPending, err := h.service.VectorPending(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	documentVectorPending, err := h.service.DocumentVectorPending(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{
		"pending":                 h.service.QueueDepth(),
		"draft_pending":           h.service.DraftQueueDepth(),
		"vector_pending":          vectorPending,
		"document_vector_pending": documentVectorPending,
	})
}

// libraryStats reports whole-library counts for the header and the vector
// coverage page.
func (h *Handler) libraryStats(c *gin.Context) {
	stats, err := h.service.Stats(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, stats)
}

func (h *Handler) reindexVectors(c *gin.Context) {
	var request struct {
		Limit int `json:"limit"`
	}
	if c.Request.ContentLength != 0 {
		if err := decodeStrictJSON(c, &request); err != nil {
			respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "only an optional limit is accepted", Cause: err})
			return
		}
	}
	result, err := h.service.ReindexAllVectors(c.Request.Context(), request.Limit)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, result)
}

func (h *Handler) listPlatforms(c *gin.Context) {
	items, err := h.service.ListPlatforms(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, items)
}

func (h *Handler) updatePlatformStyle(c *gin.Context) {
	var request service.PlatformStyleInput
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "style_rules, the numeric limits, and if_revision are required", Cause: err})
		return
	}
	platform, err := h.service.UpdatePlatformStyle(c.Request.Context(), c.Param("id"), request)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, platform)
}

func (h *Handler) resetPlatformStyle(c *gin.Context) {
	var request struct {
		IfRevision int `json:"if_revision"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "if_revision is required", Cause: err})
		return
	}
	platform, err := h.service.ResetPlatformStyle(c.Request.Context(), c.Param("id"), request.IfRevision)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, platform)
}

func (h *Handler) listPrompts(c *gin.Context) {
	items, err := h.service.ListPrompts(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, items)
}

// listPromptLayers returns the Markdown prompt layers compiled into the binary
// together with the owner's replacements, so settings can show the text in
// effect without reading the source tree.
func (h *Handler) listPromptLayers(c *gin.Context) {
	layers, err := h.service.ListPromptLayers(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, layers)
}

func (h *Handler) savePromptLayer(c *gin.Context) {
	var request service.PromptLayerInput
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "text and if_revision are required", Cause: err})
		return
	}
	layer, err := h.service.SavePromptLayer(c.Request.Context(), c.Param("key"), request)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, layer)
}

func (h *Handler) resetPromptLayer(c *gin.Context) {
	var request struct {
		IfRevision int `json:"if_revision"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "if_revision is required", Cause: err})
		return
	}
	layer, err := h.service.ResetPromptLayer(c.Request.Context(), c.Param("key"), request.IfRevision)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, layer)
}
