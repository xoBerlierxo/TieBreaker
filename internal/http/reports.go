package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/report"
)

// ReportService is what the report handlers need. *report.Service implements it.
type ReportService interface {
	ForSession(ctx context.Context, sessionID string) (report.View, error)
	ByShareID(ctx context.Context, shareID string) (report.View, error)
}

type reportHandler struct {
	logger  *slog.Logger
	reports ReportService
}

// forSession handles GET /v1/sessions/:id/report (winner / deadlock / none_fit).
func (h reportHandler) forSession(c *gin.Context) {
	v, err := h.reports.ForSession(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// shared handles GET /r/:shareId: the public, read-only report (SPEC §6.7).
// The view contains no session ID, so a share link can't be used to change
// or even find the session.
func (h reportHandler) shared(c *gin.Context) {
	v, err := h.reports.ByShareID(c.Request.Context(), c.Param("shareId"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, v)
}
