package httpapi

import (
	"context"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"clovergo/internal/service"

	"github.com/gin-gonic/gin"
)

// pickFolder opens the system folder dialog on this machine (the server is the
// owner's own computer, bound to 127.0.0.1) and returns the chosen path. An
// empty path means the dialog was cancelled. It only returns a string: nothing
// is read or registered until the client calls POST /roots.
func (h *Handler) pickFolder(c *gin.Context) {
	if runtime.GOOS != "windows" {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidState, Message: "folder dialog is only available on Windows; type the path instead"})
		return
	}
	const script = `[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
Add-Type -AssemblyName System.Windows.Forms
$owner = New-Object System.Windows.Forms.Form
$owner.TopMost = $true
$owner.ShowInTaskbar = $false
$owner.StartPosition = 'CenterScreen'
$owner.Size = New-Object System.Drawing.Size(1, 1)
$owner.Opacity = 0
$owner.Show()
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.ShowNewFolderButton = $false
if ($dialog.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($dialog.SelectedPath) }
$owner.Close()`
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInternal, Message: "folder dialog failed", Cause: err})
		return
	}
	path := strings.TrimSpace(strings.TrimPrefix(string(out), "\ufeff"))
	respond(c, http.StatusOK, gin.H{"path": path})
}

func (h *Handler) listRoots(c *gin.Context) {
	roots, err := h.service.ListRoots(c.Request.Context())
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, roots)
}

func (h *Handler) addRoot(c *gin.Context) {
	var request struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "path is required", Cause: err})
		return
	}
	root, err := h.service.AddRoot(c.Request.Context(), request.Path, request.Name)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusCreated, root)
}

func (h *Handler) deleteRoot(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	if err := h.service.DeleteRoot(c.Request.Context(), id); err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"deleted": true})
}

// scanRoot reports what is new, changed, skipped or gone in a folder. It
// imports nothing and calls no model.
func (h *Handler) scanRoot(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	report, err := h.service.ScanRoot(c.Request.Context(), id)
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, report)
}

// importRootFile imports one file of a folder (one summary call). The client
// calls it once per file so one failure does not stop the rest.
func (h *Handler) importRootFile(c *gin.Context) {
	id, err := positiveID(c.Param("id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	var request struct {
		Path  string `json:"path"`
		Topic string `json:"topic"`
	}
	if err := decodeStrictJSON(c, &request); err != nil {
		respondError(c, h.log, &service.AppError{Code: service.CodeInvalidInput, Message: "path is required", Cause: err})
		return
	}
	result, err := h.service.ImportRootFile(c.Request.Context(), id, request.Path, request.Topic)
	h.respondImport(c, result, err)
}
