package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

type upload struct {
	filename string
	data     []byte
}

// readUpload reads the multipart "file" field within the configured upload
// limit and writes the error response itself when it returns false.
func (h *Handler) readUpload(c *gin.Context) (upload, bool) {
	return h.readUploadField(c, "file", false)
}

// readUploadField reads one multipart file field. When optional is set a
// missing field yields an empty upload instead of an error.
func (h *Handler) readUploadField(c *gin.Context, field string, optional bool) (upload, bool) {
	maxBody := h.service.MaxUploadBytes() + 1<<20
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBody)
	return h.readFormFile(c, field, optional)
}

// readFormFile reads one multipart file field without touching the body limit,
// so a handler that takes several files can set one limit for all of them.
func (h *Handler) readFormFile(c *gin.Context, field string, optional bool) (upload, bool) {
	fileHeader, err := c.FormFile(field)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			respondError(c, h.log, &service.AppError{Code: service.CodeFileTooLarge, Message: "request body exceeds upload limit", Cause: err})
			return upload{}, false
		}
		if optional && errors.Is(err, http.ErrMissingFile) {
			return upload{}, true
		}
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "multipart field " + field + " is required", Cause: err})
		return upload{}, false
	}
	file, err := fileHeader.Open()
	if err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "uploaded file cannot be opened", Cause: err})
		return upload{}, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, h.service.MaxUploadBytes()+1))
	if err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "uploaded file cannot be read", Cause: err})
		return upload{}, false
	}
	if int64(len(data)) > h.service.MaxUploadBytes() {
		respondError(c, h.log, &service.AppError{Code: service.CodeFileTooLarge, Message: "uploaded file " + field + " exceeds upload limit"})
		return upload{}, false
	}
	return upload{filename: fileHeader.Filename, data: data}, true
}

// decodeStrictJSON accepts exactly one JSON object with no unknown fields.
func decodeStrictJSON(c *gin.Context, target any) error {
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain exactly one JSON object")
		}
		return err
	}
	return nil
}

func positiveID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, &service.AppError{Code: service.CodeInvalidInput, Message: "id must be a positive integer", Cause: err}
	}
	return id, nil
}

func queryLimit(c *gin.Context) int {
	limit, _ := strconv.Atoi(c.Query("limit"))
	return limit
}

func queryOffset(c *gin.Context) int {
	offset, _ := strconv.Atoi(c.Query("offset"))
	if offset < 0 {
		return 0
	}
	return offset
}

func respondZIP(c *gin.Context, data []byte, filename string) {
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "application/zip", data)
}
