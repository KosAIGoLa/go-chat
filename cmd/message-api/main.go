package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ck-chat/ck-chat/internal/app"
	"github.com/ck-chat/ck-chat/internal/auth"
	"github.com/ck-chat/ck-chat/internal/conversation"
	"github.com/ck-chat/ck-chat/internal/message"
	"github.com/ck-chat/ck-chat/internal/sequence"
)

func main() {
	app.Main(app.Service{Name: "message-api", Run: run})
}

func run(ctx context.Context, rt app.Runtime) error {
	svc := message.NewService(sequence.NewAllocator(), message.NewMemoryStore())
	secret := rt.Config.Auth.TokenSecret
	if secret == "" {
		secret = "local-dev-secret-change-me"
	}
	ttl := 24 * time.Hour
	if rt.Config.Auth.TokenTTL != "" {
		if parsed, err := time.ParseDuration(rt.Config.Auth.TokenTTL); err == nil {
			ttl = parsed
		}
	}
	tokens := auth.NewTokenService(secret)
	if rt.Config.Service.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	auth.NewHTTPHandler(tokens, ttl).Register(router)
	convStore := conversation.NewStore()
	protected := router.Group("/")
	protected.Use(auth.Middleware(tokens))
	message.NewHTTPHandler(svc).Register(protected)
	conversation.NewHTTPHandler(convStore).Register(protected)
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/readyz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ready"}) })

	addr := rt.Config.HTTP.Addr
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		rt.Logger.Info("message api listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
