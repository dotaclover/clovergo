package httpapi

import (
	"net/http"

	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *Handler) ask(c *gin.Context) {
	var request struct {
		Question string `json:"question"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "a JSON body with question is required", Cause: err})
		return
	}
	answer, err := h.service.Ask(c.Request.Context(), request.Question)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, answer)
}
