package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/josephcopenhaver/melody-bot/internal/logging"
	"github.com/josephcopenhaver/melody-bot/internal/service"
	"github.com/josephcopenhaver/melody-bot/internal/service/config"
	"github.com/josephcopenhaver/melody-bot/internal/service/server"
	"golang.org/x/exp/slog"
)

// rootContext returns a context that is canceled when the
// system process receives an interrupt, sigint, or sigterm
//
// Also returns a function that can be used to cancel the context.
func rootContext() (context.Context, func()) { //nolint:gocritic

	ctx, cancel := context.WithCancel(context.Background())

	procDone := make(chan os.Signal, 1)

	signal.Notify(procDone, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		defer cancel()

		done := ctx.Done()

		var requester string
		select {
		case <-procDone:
			requester = "user"
		case <-done:
			requester = "process"
		}

		slog.Warn(
			"shutdown requested",
			"requester", requester,
		)
	}()

	return ctx, cancel
}

var GitSHA string
var Version string

func panicLog(msg string, vargs ...any) {
	slog.Error(msg, vargs...)
	panic(errors.New(msg))
}

func panicErrLog(err error, msg string, vargs ...any) {
	slog.With("error", err).Error(msg, vargs...)
	panic(fmt.Errorf("%s: %w", msg, err))
}

func main() {
	var ctx context.Context
	{
		newCtx, cancel := rootContext()
		defer cancel()

		ctx = newCtx
	}

	logLevelStr := os.Getenv("LOG_LEVEL")
	if logLevelStr != "" {
		if err := logging.SetDefaultLevel(logLevelStr); err != nil {
			panicLog(
				"invalid log level",
				"LOG_LEVEL", logLevelStr,
			)
		}
	}

	service.Version = Version
	service.Commit = GitSHA
	slog.InfoContext(ctx,
		"melody-bot initializing",
		"Version", service.Version,
		"Commit", service.Commit,
	)

	conf, err := config.New()
	if err != nil {
		panicErrLog(err, "failed to read configuration")
	}

	server := server.New()
	if err := server.SetConfig(conf); err != nil {
		panicErrLog(err, "failed to load configuration")
	}

	if err := server.Handlers(ctx); err != nil {
		panicErrLog(err, "failed to register handlers")
	}

	slog.InfoContext(ctx, "starting listener")

	if err := server.ListenAndServe(ctx); err != nil {
		panicErrLog(err, "server stopped unexpectedly")
	}

	slog.WarnContext(ctx, "server shutdown complete")
}
