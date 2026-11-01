package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) listAgentRuns(c *gin.Context) {
	runs, err := h.service.ListRecentRuns(c.Request.Context(), queryLimit(c))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, runs)
}

func (h *Handler) getAgentRun(c *gin.Context) {
	run, err := h.service.GetAgentRun(c.Request.Context(), c.Param("run_id"))
	if err != nil {
		respondError(c, h.log, err)
		return
	}
	respond(c, http.StatusOK, run)
}
