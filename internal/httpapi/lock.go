package httpapi

import (
	"net/http"

	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

// tokenHeader carries the unlock token the client keeps in localStorage.
const tokenHeader = "X-CloverGo-Token"

// requireUnlock refuses every request that does not carry the current unlock
// token. With no password set it lets everything through.
func (h *Handler) requireUnlock() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := h.service.VerifyToken(c.Request.Context(), c.GetHeader(tokenHeader)); err != nil {
			respondError(c, h.log, err)
			c.Abort()
			return
		}
		c.Next()
	}
}

// authStatus tells the client whether a password is set and whether the
// token it holds is still good.
func (h *Handler) authStatus(c *gin.Context) {
	enabled, err := h.service.LockStatus(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	unlocked := !enabled || h.service.VerifyToken(c.Request.Context(), c.GetHeader(tokenHeader)) == nil
	respond(c, http.StatusOK, gin.H{"enabled": enabled, "unlocked": unlocked})
}

type passwordRequest struct {
	Current  string `json:"current"`
	Password string `json:"password"`
}

func (h *Handler) unlock(c *gin.Context) {
	var request passwordRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "password is required", Cause: err})
		return
	}
	token, err := h.service.Unlock(c.Request.Context(), request.Password)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"token": token})
}

func (h *Handler) setPassword(c *gin.Context) {
	var request passwordRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "password is required", Cause: err})
		return
	}
	token, err := h.service.SetPassword(c.Request.Context(), request.Current, request.Password)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"enabled": true, "token": token})
}

func (h *Handler) removePassword(c *gin.Context) {
	var request passwordRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "current password is required", Cause: err})
		return
	}
	if err := h.service.RemovePassword(c.Request.Context(), request.Current); err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"enabled": false})
}
