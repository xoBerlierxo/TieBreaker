//go:build !integration

package httpapi

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain for unit-test runs. Integration runs (-tags=integration) use the
// TestMain in integration_test.go, which also starts the containers.
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}
