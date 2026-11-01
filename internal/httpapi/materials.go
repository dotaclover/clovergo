package httpapi

import (
	"net/http"

	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

// importMaterial takes a file and an optional topic, waits for its summary,
// and archives it. Knowledge extraction requires a separate explicit action.
func (h *Handler) importMaterial(c *gin.Context) {
	file, ok := h.readUpload(c)
	if !ok {
		return
	}
	result, err := h.service.ImportMaterial(c.Request.Context(), file.filename, file.data, c.PostForm("topic"))
	h.respondImport(c, result, err)
}

// importText takes pasted text and an optional topic, and names the material
// after its first meaningful line.
func (h *Handler) importText(c *gin.Context) {
	var request struct {
		Text  string `json:"text"`
		Topic string `json:"topic"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "text is required", Cause: err})
		return
	}
	result, err := h.service.ImportText(c.Request.Context(), request.Text, request.Topic)
	h.respondImport(c, result, err)
}

// updateDocumentTopic files an existing material under a different topic.
func (h *Handler) updateDocumentTopic(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	var request struct {
		Topic string `json:"topic"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "topic is required", Cause: err})
		return
	}
	document, err := h.service.UpdateDocumentTopic(c.Request.Context(), id, request.Topic)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, document)
}

// updateDocument renames a material.
func (h *Handler) updateDocument(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	var request struct {
		Filename string `json:"filename"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "filename is required", Cause: err})
		return
	}
	document, err := h.service.RenameDocument(c.Request.Context(), id, request.Filename)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, document)
}

// deleteDocument removes a material with its knowledge and stored file.
func (h *Handler) deleteDocument(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	if err := h.service.DeleteDocument(c.Request.Context(), id); err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"deleted": true})
}

func (h *Handler) respondImport(c *gin.Context, result service.MaterialImportResult, err error) {
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	status := http.StatusCreated
	if result.Duplicate {
		status = http.StatusOK
	}
	respond(c, status, result)
}

// reprocessMaterial queues a material again, for example after a failure. It
// returns immediately; the caller polls the document status.
func (h *Handler) reprocessMaterial(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	result, err := h.service.ReprocessDocument(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusAccepted, result)
}

func (h *Handler) listDocuments(c *gin.Context) {
	documents, err := h.service.ListDocuments(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, documents)
}

func (h *Handler) getDocument(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	document, err := h.service.GetDocument(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	knowledge, err := h.service.ListKnowledgeByDocument(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"document": document, "knowledge": knowledge})
}

// getDocumentText returns one material's stored text, re-read from disk and
// re-normalized, so the materials list can show the original behind a citation.
func (h *Handler) getDocumentText(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	source, err := h.service.GetSourceDocument(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, source)
}
