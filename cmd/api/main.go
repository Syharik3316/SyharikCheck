package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "aeza/internal/config"
    "aeza/internal/httpserver"
    "aeza/internal/queue"
    "aeza/internal/storage"
)

func main() {
    cfg := config.Load()

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    pg, err := storage.NewPostgres(ctx, cfg.PostgresDSN)
    if err != nil {
        log.Fatalf("failed to init postgres: %v", err)
    }
    defer pg.Close()

    rds, err := queue.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
    if err != nil {
        log.Fatalf("failed to init redis: %v", err)
    }
    defer func() { _ = rds.Close() }()

    router := httpserver.NewRouter(cfg, pg, rds)

    srv := &http.Server{
        Addr:              ":" + cfg.HTTPPort,
        Handler:           router,
        ReadHeaderTimeout: 10 * time.Second,
        ReadTimeout:       30 * time.Second,
        WriteTimeout:      30 * time.Second,
        IdleTimeout:       60 * time.Second,
    }

    go func() {
        log.Printf("api listening on :%s", cfg.HTTPPort)
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Printf("http server error: %v", err)
            os.Exit(1)
        }
    }()

    <-ctx.Done()
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := srv.Shutdown(shutdownCtx); err != nil {
        log.Printf("graceful shutdown error: %v", err)
    }
}




