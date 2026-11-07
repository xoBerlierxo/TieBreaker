package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// bindJSON decodes the request body into dst strictly: unknown fields and
// trailing data are rejected, so client typos fail loudly instead of being
// silently ignored. Oversized bodies come back as *http.MaxBytesError, which
// WriteError maps to 413.
func bindJSON(c *gin.Context, dst any) error {
	if c.Request.Body == nil {
		return apperr.Validation("body", "request body is required")
	}
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			return err
		}
		if errors.Is(err, io.EOF) {
			return apperr.Validation("body", "request body is required")
		}
		return apperr.Validation("body", fmt.Sprintf("invalid JSON body: %v", err))
	}
	if dec.More() {
		return apperr.Validation("body", "request body must contain a single JSON value")
	}
	return nil
}

// queryInt parses an optional integer query parameter. Missing means def.
func queryInt(c *gin.Context, name string, def int) (int, error) {
	raw, ok := c.GetQuery(name)
	if !ok || raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, apperr.Validation(name, fmt.Sprintf("%s must be an integer", name))
	}
	return n, nil
}
