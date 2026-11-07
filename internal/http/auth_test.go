package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-jwt-secret-that-is-long-enough-0123456789"

func protectedRouter() *gin.Engine {
	r := gin.New()
	r.GET("/admin/ping", requireAdmin(testJWTSecret, discardLogger()), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func signed(t *testing.T, method jwt.SigningMethod, key any, claims jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAdminAuth_Table(t *testing.T) {
	now := time.Now()
	good, err := MintAdminToken(testJWTSecret, 5*time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	base := func(mut func(jwt.MapClaims)) jwt.MapClaims {
		c := jwt.MapClaims{
			"iss": adminTokenIssuer, "aud": adminTokenAudience, "role": "admin", "sub": "test",
			"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		}
		if mut != nil {
			mut(c)
		}
		return c
	}

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"valid token", "Bearer " + good, http.StatusOK},
		{"missing header", "", http.StatusUnauthorized},
		{"not bearer", "Basic abc", http.StatusUnauthorized},
		{"garbage token", "Bearer not.a.token", http.StatusUnauthorized},
		{"wrong secret", "Bearer " + signed(t, jwt.SigningMethodHS256, []byte("another-secret-of-sufficient-length!!"), base(nil)), http.StatusUnauthorized},
		{"expired", "Bearer " + signed(t, jwt.SigningMethodHS256, []byte(testJWTSecret), base(func(c jwt.MapClaims) {
			c["iat"] = now.Add(-time.Hour).Unix()
			c["exp"] = now.Add(-time.Minute).Unix()
		})), http.StatusUnauthorized},
		{"wrong role", "Bearer " + signed(t, jwt.SigningMethodHS256, []byte(testJWTSecret), base(func(c jwt.MapClaims) { c["role"] = "user" })), http.StatusForbidden},
		{"wrong audience", "Bearer " + signed(t, jwt.SigningMethodHS256, []byte(testJWTSecret), base(func(c jwt.MapClaims) { c["aud"] = "someone-else" })), http.StatusUnauthorized},
		{"no expiry", "Bearer " + signed(t, jwt.SigningMethodHS256, []byte(testJWTSecret), base(func(c jwt.MapClaims) { delete(c, "exp") })), http.StatusUnauthorized},
		{"too long-lived", "Bearer " + signed(t, jwt.SigningMethodHS256, []byte(testJWTSecret), base(func(c jwt.MapClaims) {
			c["exp"] = now.Add(48 * time.Hour).Unix()
		})), http.StatusUnauthorized},
		{"alg none", "Bearer " + signed(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, base(nil)), http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			protectedRouter().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestMintAdminToken_RejectsLongTTL(t *testing.T) {
	if _, err := MintAdminToken(testJWTSecret, 2*time.Hour, time.Now()); err == nil {
		t.Fatal("MintAdminToken accepted a 2h TTL")
	}
}
