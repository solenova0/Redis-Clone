package main

import (
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/solenova0/Redis-Clone/internal/command"
	"github.com/solenova0/Redis-Clone/internal/server"
)

func main() {
	addr := flag.String("addr", "localhost:6379", "TCP address to listen on")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv := server.New(*addr, command.NewDispatcher(), logger)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		logger.Info("shutting down", "signal", sig.String())
		srv.Close()
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, server.ErrServerClosed) {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}
