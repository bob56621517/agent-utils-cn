// deepseek-search-fetch-mcp 把 DeepSeek 的 Search 与 Fetch 接口暴露为 MCP 工具。
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/bob56621517/deepseek-search-fetch-mcp/internal/mcp"
	"github.com/bob56621517/deepseek-search-fetch-mcp/internal/openapi"
)

var (
	version      = "dev"
	buildChannel = "local"
)

func buildVersion() string {
	if buildChannel == "" {
		return version
	}
	return version + "+" + buildChannel
}

func main() {
	if err := run(); err != nil {
		slog.Error("服务异常退出", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	logger, logCloser, err := newLogger(os.Stderr, slog.LevelInfo)
	if err != nil {
		return err
	}
	defer logCloser.Close()
	slog.SetDefault(logger)

	client := openapi.NewClient(cfg.apiURL, cfg.apiKey, openapi.Options{
		Timeout:             cfg.requestTimeout,
		MaxIdleConns:        cfg.httpMaxIdleConns,
		MaxIdleConnsPerHost: cfg.httpMaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.httpIdleConnTimeout,
		TLSHandshakeTimeout: cfg.httpTLSHandshakeTimeout,
		ResetOnError:        cfg.resetConnectionOnError,
	})
	defer client.CloseIdleConnections()

	srv := mcp.New(mcp.Options{
		Client:  client,
		Logger:  logger,
		Version: buildVersion(),
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Warn("关闭 HTTP 服务失败", "err", err)
		}
	}()

	logger.Info("服务启动", "addr", addr, "version", version, "build", buildChannel)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
