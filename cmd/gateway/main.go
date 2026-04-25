package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ck-chat/ck-chat/internal/app"
	"github.com/ck-chat/ck-chat/internal/auth"
	"github.com/ck-chat/ck-chat/internal/gateway"
	"github.com/ck-chat/ck-chat/internal/message"
	"github.com/ck-chat/ck-chat/internal/route"
	"github.com/ck-chat/ck-chat/internal/sequence"
)

func main() {
	app.Main(app.Service{Name: "gateway", Run: run})
}

func run(ctx context.Context, rt app.Runtime) error {
	if rt.Config.Service.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	secret := rt.Config.Auth.TokenSecret
	if secret == "" {
		secret = "local-dev-secret-change-me"
	}
	messageSvc := message.NewService(sequence.NewAllocator(), message.NewMemoryStore())
	gateway.NewWebSocketHandler().
		WithAuth(auth.NewTokenService(secret)).
		WithGatewayID(rt.Config.Service.Name).
		WithRouteRegistry(route.NewRegistry()).
		WithMessageSender(messageSvc).
		Register(router)
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/readyz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ready"}) })

	addr := rt.Config.HTTP.Addr
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		rt.Logger.Info("gateway listening", "addr", addr)
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
