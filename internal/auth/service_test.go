package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTokenServiceIssueVerify(t *testing.T) {
	svc := NewTokenService("secret")
	token, err := svc.Issue(Principal{TenantID: 1, UserID: 2, DeviceID: "ios", Scopes: []string{"message:send"}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := svc.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.TenantID != 1 || principal.UserID != 2 || principal.DeviceID != "ios" {
		t.Fatalf("unexpected principal: %+v", principal)
	}
}

func TestAuthMiddlewarePropagatesPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewTokenService("secret")
	token, err := svc.Issue(Principal{TenantID: 1, UserID: 2, DeviceID: "ios"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	router := gin.New()
	router.Use(Middleware(svc))
	router.GET("/", func(c *gin.Context) {
		principal, ok := PrincipalFromContext(c.Request.Context())
		if !ok || principal.UserID != 2 {
			t.Fatalf("principal missing: %+v", principal)
		}
		called = true
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("next handler not called")
	}
}

func TestAuthMiddlewareRejectsInvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewTokenService("secret")
	router := gin.New()
	router.Use(Middleware(svc))
	router.GET("/", func(c *gin.Context) { t.Fatal("should not call next") })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", rec.Code)
	}
}
