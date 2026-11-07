package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/session"
)

// SessionService is what the session handlers need. *session.Service implements it.
type SessionService interface {
	Create(ctx context.Context, userState string) (session.View, error)
	Get(ctx context.Context, id string) (session.View, error)
	AddCar(ctx context.Context, id string, version int, req session.AddCarRequest) (session.View, error)
	RemoveCar(ctx context.Context, id string, version int, itemID string) (session.View, error)
	SetConstraints(ctx context.Context, id string, version int, c session.Constraints) (session.View, error)
	NextQuestion(ctx context.Context, id string) (session.NextResponse, error)
	SubmitAnswer(ctx context.Context, id string, version int, key string, req session.AnswerRequest) (session.AnswerOutcome, error)
}

type sessionHandler struct {
	logger   *slog.Logger
	sessions SessionService
}

// parseIfMatch reads the session version from If-Match (SPEC §12). Accepts
// `"3"`, `3` and the weak form `W/"3"`. Missing → 412; malformed → 400.
func parseIfMatch(c *gin.Context) (int, error) {
	raw := strings.TrimSpace(c.GetHeader("If-Match"))
	if raw == "" {
		return 0, apperr.New(apperr.ErrPreconditionRequired, "the If-Match header with the session version is required")
	}
	raw = strings.TrimPrefix(raw, "W/")
	raw = strings.Trim(raw, `"`)
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return 0, apperr.Validation("If-Match", "If-Match must be the session version, e.g. \"3\"")
	}
	return v, nil
}

func setETag(c *gin.Context, version int) {
	c.Header("ETag", strconv.Quote(strconv.Itoa(version)))
}

func (h sessionHandler) writeView(c *gin.Context, status int, v session.View) {
	setETag(c, v.Version)
	c.JSON(status, v)
}

// create handles POST /v1/sessions.
func (h sessionHandler) create(c *gin.Context) {
	var body struct {
		UserState string `json:"user_state"`
	}
	if err := bindJSON(c, &body); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	v, err := h.sessions.Create(c.Request.Context(), strings.ToUpper(strings.TrimSpace(body.UserState)))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	h.writeView(c, http.StatusCreated, v)
}

// get handles GET /v1/sessions/:id.
func (h sessionHandler) get(c *gin.Context) {
	v, err := h.sessions.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	h.writeView(c, http.StatusOK, v)
}

// addCar handles POST /v1/sessions/:id/cars (requires If-Match).
func (h sessionHandler) addCar(c *gin.Context) {
	version, err := parseIfMatch(c)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	var req session.AddCarRequest
	if err := bindJSON(c, &req); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	v, err := h.sessions.AddCar(c.Request.Context(), c.Param("id"), version, req)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	h.writeView(c, http.StatusCreated, v)
}

// removeCar handles DELETE /v1/sessions/:id/cars/:itemId (requires If-Match).
func (h sessionHandler) removeCar(c *gin.Context) {
	version, err := parseIfMatch(c)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	v, err := h.sessions.RemoveCar(c.Request.Context(), c.Param("id"), version, c.Param("itemId"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	h.writeView(c, http.StatusOK, v)
}

// setConstraints handles PUT /v1/sessions/:id/constraints (requires If-Match).
func (h sessionHandler) setConstraints(c *gin.Context) {
	version, err := parseIfMatch(c)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	var body session.Constraints
	if err := bindJSON(c, &body); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	v, err := h.sessions.SetConstraints(c.Request.Context(), c.Param("id"), version, body)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	h.writeView(c, http.StatusOK, v)
}

// nextQuestion handles GET /v1/sessions/:id/next-question.
func (h sessionHandler) nextQuestion(c *gin.Context) {
	next, err := h.sessions.NextQuestion(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, next)
}

// submitAnswer handles POST /v1/sessions/:id/answers (requires Idempotency-Key
// and If-Match). A replayed request gets the original response and the header
// Idempotent-Replayed: true.
func (h sessionHandler) submitAnswer(c *gin.Context) {
	key := c.GetHeader("Idempotency-Key")
	if err := session.ValidateIdempotencyKey(key); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	version, err := parseIfMatch(c)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	var req session.AnswerRequest
	if err := bindJSON(c, &req); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	out, err := h.sessions.SubmitAnswer(c.Request.Context(), c.Param("id"), version, key, req)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	if out.Replayed {
		c.Header("Idempotent-Replayed", "true")
	} else {
		setETag(c, out.Version)
	}
	c.Data(out.Status, "application/json; charset=utf-8", out.Body)
}
