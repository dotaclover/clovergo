package httpapi

import (
	"net/http"

	"clovergo/internal/repository"
	"clovergo/internal/service"
	localtools "clovergo/internal/tools"

	"github.com/gin-gonic/gin"
)

func (h *Handler) listKnowledge(c *gin.Context) {
	items, total, err := h.service.ListKnowledge(c.Request.Context(), service.KnowledgeQuery{
		Kind:     c.Query("kind"),
		Topic:    c.Query("topic"),
		Language: c.Query("language"),
		Usage:    c.Query("usage"),
		Limit:    queryLimit(c),
		Offset:   queryOffset(c),
	})
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"items": items, "total": total})
}

// getKnowledge returns one item, used by the draft screen to show which
// knowledge was handed over from the list.
func (h *Handler) getKnowledge(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	item, err := h.service.GetKnowledge(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, item)
}

// markKnowledgeRead records one read of an item and answers with the new total.
// The UI calls it when an item is opened, which is what makes "已读 N 次"
// meaningful: it counts reads, not edits.
func (h *Handler) markKnowledgeRead(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	count, err := h.service.MarkKnowledgeRead(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"id": id, "read_count": count})
}

// listTopics returns the shelves in use with their knowledge counts, so the
// knowledge page can render a filter from one call.
func (h *Handler) listTopics(c *gin.Context) {
	topics, err := h.service.ListTopics(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, topics)
}

// updateKnowledge rewrites one item's editable fields. It answers with the
// stored item, including the recomputed-provenance fields, so the caller never
// has to guess what was saved.
func (h *Handler) updateKnowledge(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	var request struct {
		Kind  string   `json:"kind"`
		Title string   `json:"title"`
		Body  string   `json:"body"`
		Tags  []string `json:"tags"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{
			Code: service.CodeInvalidInput, Message: "knowledge accepts kind, title, body and tags", Cause: err,
		})
		return
	}
	item, err := h.service.UpdateKnowledge(c.Request.Context(), id, repository.KnowledgeEdit{
		Kind: request.Kind, Title: request.Title, Body: request.Body, Tags: request.Tags,
	})
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, item)
}

func (h *Handler) deleteKnowledge(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	if err := h.service.DeleteKnowledge(c.Request.Context(), id); err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"id": id, "status": "deleted"})
}

func (h *Handler) search(c *gin.Context) {
	items, err := h.service.Search(c.Request.Context(), c.Query("q"), queryLimit(c), service.KnowledgeScope{
		Topic: c.Query("topic"), Language: c.Query("language"),
	})
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, items)
}

func (h *Handler) searchSources(c *gin.Context) {
	var request localtools.SearchSourcesInput
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{
			Code: service.CodeToolInputInvalid, Message: "search_sources accepts only query and limit", Cause: err,
		})
		return
	}
	items, err := h.service.SearchSources(c.Request.Context(), request)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, items)
}

func (h *Handler) getSourceDocument(c *gin.Context) {
	var request struct {
		DocumentID int64 `json:"document_id"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{
			Code: service.CodeToolInputInvalid, Message: "get_document accepts only document_id", Cause: err,
		})
		return
	}
	source, err := h.service.GetSourceDocument(c.Request.Context(), request.DocumentID)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, source)
}
