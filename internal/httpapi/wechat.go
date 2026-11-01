package httpapi

import (
	"net/http"

	"clovergo/internal/domain"
	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

// getWechatSettings returns the Official Account configuration shown on the
// settings screen. AppSecret is omitted and proxy credentials are masked.
func (h *Handler) getWechatSettings(c *gin.Context) {
	settings, err := h.service.GetWechatSettings(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, settings)
}

func (h *Handler) saveWechatSettings(c *gin.Context) {
	var request domain.WechatSettings
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "the settings fields are required", Cause: err})
		return
	}
	settings, err := h.service.SaveWechatSettings(c.Request.Context(), request)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, settings)
}

// testWechatSettings fetches an access token with the stored credentials, which
// is the cheapest proof that the configuration works.
func (h *Handler) testWechatSettings(c *gin.Context) {
	if err := h.service.TestWechat(c.Request.Context()); err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"status": "ok"})
}
