package httpapi

import (
	"mime"
	"net/http"
	"strconv"

	"clovergo/internal/platforms"
	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *Handler) agentChat(c *gin.Context) {
	var request service.AgentChatInput
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "messages is required", Cause: err})
		return
	}
	reply, err := h.service.AgentChat(c.Request.Context(), request)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, reply)
}

// listAgentQA returns the paged history of answered questions for the 知识问答
// page: the question, the answer, its citations and the tool steps.
func (h *Handler) listAgentQA(c *gin.Context) {
	items, total, err := h.service.ListQAHistory(c.Request.Context(), queryLimit(c), queryOffset(c))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"items": items, "total": total})
}

// pushDraftToWechat accepts an empty body (use the default cover) or a
// multipart body with an optional "cover" image and, for a picture post,
// optional "image1".."image3" files that replace the generated pictures.
// ?mode=publish additionally submits the pushed draft for publishing; without
// it the draft only enters the draft box.
func (h *Handler) pushDraftToWechat(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	var coverName string
	var cover []byte
	images := map[int][]byte{}
	if mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type")); err == nil && mediaType == "multipart/form-data" {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (1+int64(platforms.GalleryImageMaxCount))*h.service.MaxUploadBytes()+1<<20)
		upload, ok := h.readFormFile(c, "cover", true)
		if !ok {
			return
		}
		coverName, cover = upload.filename, upload.data
		for slot := 1; slot <= platforms.GalleryImageMaxCount; slot++ {
			picture, ok := h.readFormFile(c, "image"+strconv.Itoa(slot), true)
			if !ok {
				return
			}
			if len(picture.data) > 0 {
				images[slot] = picture.data
			}
		}
	}
	draft, err := h.service.PushDraftToWechat(c.Request.Context(), id, c.Query("mode") == "publish", coverName, cover, images)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, draft)
}
