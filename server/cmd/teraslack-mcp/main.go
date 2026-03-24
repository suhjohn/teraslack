package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/suhjohn/teraslack/internal/teraslackmcp"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := teraslackmcp.LoadConfigFromEnv()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	server, err := teraslackmcp.NewServer(cfg, logger)
	if err != nil {
		logger.Error("create server", "error", err)
		os.Exit(1)
	}

	if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		logger.Error("serve MCP", "error", err)
		os.Exit(1)
	}
}
