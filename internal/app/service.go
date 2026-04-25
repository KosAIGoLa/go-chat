package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ck-chat/ck-chat/internal/config"
)

type Service struct {
	Name string
	Run  func(context.Context, Runtime) error
}

type Runtime struct {
	Config config.Config
	Logger *slog.Logger
}

func Main(service Service) {
	configPath := flag.String("config", "configs/local.yaml", "path to yaml config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	if service.Name == "" {
		service.Name = cfg.Service.Name
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(
		"service", service.Name,
		"env", cfg.Service.Env,
	)
	rt := Runtime{Config: cfg, Logger: logger}

	logger.Info("starting service", "config", *configPath)

	if service.Run == nil {
		if err := RunHealthServer(ctx, rt); err != nil {
			logger.Error("health server failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := service.Run(ctx, rt); err != nil {
		logger.Error("service failed", "error", err)
		os.Exit(1)
	}
	logger.Info(fmt.Sprintf("%s stopped", service.Name))
}

func RunHealthServer(ctx context.Context, rt Runtime) error {
	if rt.Config.Service.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	addr := rt.Config.HTTP.Addr
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: router, ReadHeaderTimeout: 5 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		rt.Logger.Info("health server listening", "addr", addr)
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
