package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

func TestParseIfMatch(t *testing.T) {
	tests := []struct {
		header string
		want   int
		kind   error
	}{
		{`"3"`, 3, nil},
		{`3`, 3, nil},
		{`W/"3"`, 3, nil},
		{` "12" `, 12, nil},
		{"", 0, apperr.ErrPreconditionRequired},
		{`"abc"`, 0, apperr.ErrValidation},
		{`"0"`, 0, apperr.ErrValidation},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		if tt.header != "" {
			c.Request.Header.Set("If-Match", tt.header)
		}
		got, err := parseIfMatch(c)
		if tt.kind != nil {
			if !errors.Is(err, tt.kind) {
				t.Errorf("If-Match %q: err = %v, want %v", tt.header, err, tt.kind)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("If-Match %q = %d, %v; want %d", tt.header, got, err, tt.want)
		}
	}
}
