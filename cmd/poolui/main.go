package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"whale2api/internal/config"
	"whale2api/internal/pooldb"
	"whale2api/internal/poolui"
)

func main() {
	if err := config.LoadDotEnv(); err != nil {
		config.Logger.Warn("[dotenv] load failed", "error", err)
	}
	config.RefreshLogger()

	ctx := context.Background()
	db, err := pooldb.ConnectFromEnv(ctx)
	if err != nil {
		config.Logger.Error("database connect failed", "error", err)
		os.Exit(1)
	}
	if db == nil {
		config.Logger.Error("database unavailable for pool UI", "path", pooldb.DatabasePath())
		os.Exit(1)
	}
	defer db.Close()

	srv, err := poolui.NewServer(db)
	if err != nil {
		config.Logger.Error("pool ui init failed", "error", err)
		os.Exit(1)
	}

	port := strings.TrimSpace(os.Getenv("POOL_UI_PORT"))
	if port == "" {
		port = "5010"
	}
	httpSrv := &http.Server{
		Addr:              "0.0.0.0:" + port,
		Handler:           srv.Router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	url := fmt.Sprintf("http://127.0.0.1:%s", port)

	go func() {
		config.Logger.Info("pool ui listening", "addr", httpSrv.Addr, "url", url)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			config.Logger.Error("pool ui stopped", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}
