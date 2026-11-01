package httpapi

import (
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

// localOnly rejects requests that a browser could send on behalf of another
// site. Listening on 127.0.0.1 does not stop a web page from posting to the
// local service (CSRF) or reading it through DNS rebinding, so the Host header
// must name this machine and state-changing requests must be same-origin.
func localOnly(log *slog.Logger, listenAddress string) gin.HandlerFunc {
	allowedHosts := map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true}
	if host, _, err := net.SplitHostPort(listenAddress); err == nil && host != "" && host != "0.0.0.0" && host != "::" {
		allowedHosts[strings.ToLower(host)] = true
	}
	hostAllowed := func(hostPort string) bool {
		host := hostPort
		if parsedHost, _, err := net.SplitHostPort(hostPort); err == nil {
			host = parsedHost
		}
		return allowedHosts[strings.ToLower(strings.Trim(host, "[]"))]
	}

	return func(c *gin.Context) {
		if !hostAllowed(c.Request.Host) {
			rejectRequest(c, log, http.StatusForbidden, service.CodeForbiddenOrigin, "request host is not this local service")
			return
		}
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		if strings.EqualFold(c.GetHeader("Sec-Fetch-Site"), "cross-site") {
			rejectRequest(c, log, http.StatusForbidden, service.CodeForbiddenOrigin, "cross-site requests are not allowed")
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, c.Request.Host) || !hostAllowed(parsed.Host) {
				rejectRequest(c, log, http.StatusForbidden, service.CodeForbiddenOrigin, "cross-origin requests are not allowed")
				return
			}
		}
		c.Next()
	}
}

// multipartRoutes are the only routes that take file uploads.
var multipartRoutes = map[string]bool{
	"/api/v1/materials":               true,
	"/api/v1/media-drafts/:id/wechat": true,
}

// requireJSON keeps JSON endpoints out of reach of "simple" cross-site
// requests, which can only send text/plain, form, or multipart bodies.
func requireJSON(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			c.Next()
			return
		}
		contentType := c.GetHeader("Content-Type")
		if contentType == "" && c.Request.ContentLength == 0 {
			// Body-less actions such as reprocess, confirm, and reject.
			c.Next()
			return
		}
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err == nil && (mediaType == "application/json" || (mediaType == "multipart/form-data" && multipartRoutes[c.FullPath()])) {
			c.Next()
			return
		}
		rejectRequest(c, log, http.StatusUnsupportedMediaType, service.CodeUnsupportedMediaType,
			"request body must be application/json or multipart/form-data")
	}
}

func rejectRequest(c *gin.Context, log *slog.Logger, status int, code, message string) {
	log.WarnContext(c.Request.Context(), "request rejected", "code", code, "method", c.Request.Method,
		"path", c.Request.URL.Path, "host", c.Request.Host, "origin", c.GetHeader("Origin"))
	c.AbortWithStatusJSON(status, errorEnvelope{
		Error: errorBody{Code: code, Message: message}, RequestID: c.GetString(requestIDKey),
	})
}
