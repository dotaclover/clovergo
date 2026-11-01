package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

const requestIDKey = "request_id"

type successEnvelope struct {
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error     errorBody `json:"error"`
	RequestID string    `json:"request_id"`
	RunID     string    `json:"run_id,omitempty"`
}

func respond(c *gin.Context, status int, data any) {
	c.JSON(status, successEnvelope{Data: data, RequestID: c.GetString(requestIDKey)})
}

func respondError(c *gin.Context, log *slog.Logger, err error) {
	status, code, message := http.StatusInternalServerError, service.CodeInternal, "internal server error"
	runID := ""
	var appErr *service.AppError
	if errors.As(err, &appErr) {
		code, message = appErr.Code, appErr.Message
		runID = appErr.RunID
		status = statusForCode(code)
	}
	if status >= 500 || status == http.StatusTooManyRequests {
		log.ErrorContext(c.Request.Context(), "request failed", "code", code, "error", err)
	}
	c.JSON(status, errorEnvelope{
		Error: errorBody{Code: code, Message: message}, RequestID: c.GetString(requestIDKey), RunID: runID,
	})
}

func statusForCode(code string) int {
	switch code {
	case service.CodeInvalidInput, service.CodeDocumentEmpty, service.CodeToolInputInvalid:
		return http.StatusBadRequest
	case service.CodeFileTooLarge:
		return http.StatusRequestEntityTooLarge
	case service.CodeUnsupportedFormat, service.CodeUnsupportedMediaType:
		return http.StatusUnsupportedMediaType
	case service.CodeDocumentNotFound, service.CodeKnowledgeNotFound,
		service.CodeSourceNotFound, service.CodeRootNotFound, service.CodeRunNotFound, service.CodeMediaDraftNotFound:
		return http.StatusNotFound
	case service.CodeRevisionConflict, service.CodeInvalidState, service.CodeRunInterrupted:
		return http.StatusConflict
	case service.CodeForbiddenOrigin:
		return http.StatusForbidden
	case service.CodeNoGroundedKnowledge, service.CodeAgentStepLimit:
		return http.StatusUnprocessableEntity
	case service.CodeArkNotConfigured, service.CodeEmbeddingNotConfigured, service.CodeWechatNotConfigured:
		return http.StatusServiceUnavailable
	case service.CodeAuthRequired, service.CodePasswordIncorrect:
		return http.StatusUnauthorized
	case service.CodeArkRateLimited, service.CodeTooManyAttempts:
		return http.StatusTooManyRequests
	case service.CodeArkTimeout:
		return http.StatusGatewayTimeout
	case service.CodeArkUpstream, service.CodeArkAuthFailed, service.CodeArkInvalidResponse, service.CodeWechatUpstream:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		idBytes := make([]byte, 12)
		if _, err := rand.Read(idBytes); err != nil {
			id := fmt.Sprintf("local-%d", time.Now().UnixNano())
			c.Set(requestIDKey, id)
			c.Header("X-Request-ID", id)
			c.Next()
			return
		}
		id := hex.EncodeToString(idBytes)
		c.Set(requestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func accessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		log.InfoContext(c.Request.Context(), "http request",
			"request_id", c.GetString(requestIDKey),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}
}

func recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorContext(c.Request.Context(), "panic recovered", "panic", recovered, "stack", string(debug.Stack()))
				c.AbortWithStatusJSON(http.StatusInternalServerError, errorEnvelope{
					Error:     errorBody{Code: service.CodeInternal, Message: "internal server error"},
					RequestID: c.GetString(requestIDKey),
				})
			}
		}()
		c.Next()
	}
}
