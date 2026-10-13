package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ocm/internal/application"
	"ocm/internal/config"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("ocm stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("c", "config.json", "configuration file")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println("ocm " + version)
		return nil
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if cfg.LogLevel != "" {
		if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
			return err
		}
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	service, err := application.New(cfg, logger)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := service.Start(); err != nil {
		return err
	}
	<-ctx.Done()
	return service.Close()
}
