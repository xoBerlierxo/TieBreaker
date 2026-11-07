package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// fakePinger is a Pinger that returns a fixed error (nil means healthy).
type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testDeps() Deps {
	return Deps{
		Logger:       discardLogger(),
		Postgres:     fakePinger{},
		Redis:        fakePinger{},
		MaxBodyBytes: 1 << 20,
	}
}

// do sends a request to the handler and returns the recorded response.
func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decodeError parses the standard error shape from a response body.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) ErrorPayload {
	t.Helper()
	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not the error shape: %v; body=%s", err, rec.Body.String())
	}
	return body.Error
}
