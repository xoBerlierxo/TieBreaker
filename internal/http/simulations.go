package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/jobs"
	"github.com/xoBerlierxo/TieBreaker/internal/session"
)

// SimulationService is what the simulation handlers need. *session.Service implements it.
type SimulationService interface {
	StartSimulation(ctx context.Context, sessionID string, version int) (jobs.Run, error)
	GetRun(ctx context.Context, runID string) (session.RunView, error)
	CancelRun(ctx context.Context, runID string) (jobs.Run, error)
}

// ProgressSource subscribes to a run's live progress events.
type ProgressSource interface {
	Subscribe(ctx context.Context, runID string) (<-chan jobs.ProgressEvent, func(), error)
}

// SSE timing.
const (
	ssePing        = 15 * time.Second // keep proxies from closing an idle stream
	ssePoll        = 5 * time.Second  // re-read Postgres in case a pub/sub message was lost
	sseMaxDuration = 10 * time.Minute // a client that needs longer reconnects
)

type simulationHandler struct {
	logger   *slog.Logger
	sims     SimulationService
	progress ProgressSource
}

// start handles POST /v1/sessions/:id/simulations → 202 {run_id} (SPEC §11).
func (h simulationHandler) start(c *gin.Context) {
	version, err := parseIfMatch(c)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	run, err := h.sims.StartSimulation(c.Request.Context(), c.Param("id"), version)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.Header("Location", "/v1/simulations/"+run.ID)
	c.JSON(http.StatusAccepted, gin.H{
		"run_id":     run.ID,
		"status":     run.Status,
		"status_url": "/v1/simulations/" + run.ID,
		"events_url": "/v1/simulations/" + run.ID + "/events",
	})
}

// get handles GET /v1/simulations/:id.
func (h simulationHandler) get(c *gin.Context) {
	v, err := h.sims.GetRun(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// cancel handles POST /v1/simulations/:id/cancel.
func (h simulationHandler) cancel(c *gin.Context) {
	run, err := h.sims.CancelRun(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"run_id": run.ID, "status": run.Status, "cancel_requested": run.CancelRequested})
}

// events handles GET /v1/simulations/:id/events as server-sent events.
//
// Order matters: SUBSCRIBE first, THEN read the current state from Postgres.
// Reading first could miss an update published in between; subscribing first
// can at worst deliver an update we already know, which is harmless. A client
// that reconnects gets the latest state from Postgres immediately.
func (h simulationHandler) events(c *gin.Context) {
	runID := c.Param("id")
	ctx, cancel := context.WithTimeout(c.Request.Context(), sseMaxDuration)
	defer cancel()

	if _, err := h.sims.GetRun(ctx, runID); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	updates, stop, err := h.progress.Subscribe(ctx, runID)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	defer stop()

	// Lift the server's write timeout for this one long-lived response.
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // disable proxy buffering (nginx)
	c.Status(http.StatusOK)

	last := jobs.ProgressEvent{Progress: -1}
	send := func(ev jobs.ProgressEvent) bool {
		if ev.Status == last.Status && ev.Progress == last.Progress {
			return !ev.Status.Terminal()
		}
		last = ev
		b, _ := json.Marshal(ev)
		if _, err := fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", b); err != nil {
			return false
		}
		c.Writer.Flush()
		return !ev.Status.Terminal()
	}
	current := func() (jobs.ProgressEvent, bool) {
		v, err := h.sims.GetRun(ctx, runID)
		if err != nil {
			return jobs.ProgressEvent{}, false
		}
		ev := jobs.ProgressEvent{RunID: runID, Status: v.Status, Progress: v.Progress}
		if v.Error != nil {
			ev.Error = *v.Error
		}
		return ev, true
	}

	if ev, ok := current(); !ok || !send(ev) {
		return
	}
	ping := time.NewTicker(ssePing)
	defer ping.Stop()
	poll := time.NewTicker(ssePoll)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-updates:
			if !ok {
				return
			}
			if !send(ev) {
				return
			}
		case <-poll.C:
			if ev, ok := current(); !ok || !send(ev) {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(c.Writer, ": ping\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}
