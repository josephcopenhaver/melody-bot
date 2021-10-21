package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/josephcopenhaver/melody-bot/internal/logging"
	"github.com/josephcopenhaver/melody-bot/internal/service"
	"github.com/josephcopenhaver/melody-bot/internal/service/config"
	"github.com/josephcopenhaver/melody-bot/internal/service/server"
	"github.com/rs/zerolog/log"
)

// rootContext returns a context that is canceled when the
// system process receives an interrupt, sigint, or sigterm
//
// Also returns a function that can be used to cancel the context.
func rootContext() (context.Context, func()) {

	ctx, cancel := context.WithCancel(context.Background())

	procDone := make(chan os.Signal, 1)

	signal.Notify(procDone, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		defer cancel()

		done := ctx.Done()

		requester := "unknown"
		select {
		case <-procDone:
			requester = "user"
		case <-done:
			requester = "process"
		}

		log.Warn().
			Str("requester", requester).
			Msg("shutdown requested")
	}()

	return ctx, cancel
}

var GitSHA string
var Version string

func main() {
	var ctx context.Context
	{
		newCtx, cancel := rootContext()
		defer cancel()

		ctx = newCtx
	}

	logLevelStr := os.Getenv("LOG_LEVEL")
	if logLevelStr != "" {
		if err := logging.SetGlobalLevel(logLevelStr); err != nil {
			log.Panic().
				Str("LOG_LEVEL", logLevelStr).
				Msg("invalid log level")
		}
	}

	service.Version = Version
	service.Commit = GitSHA
	log.Info().
		Str("Version", service.Version).
		Str("Commit", service.Commit).
		Msg("melody-bot initializing")

	conf, err := config.New()
	if err != nil {
		log.Panic().
			Err(err).
			Msg("failed to read configuration")
	}

	server := server.New()
	if err := server.SetConfig(conf); err != nil {
		log.Panic().
			Err(err).
			Msg("failed to load configuration")
	}

	if err := server.Handlers(); err != nil {
		log.Panic().
			Err(err).
			Msg("failed to register handlers")
	}

	service.NicenessInit()

	// set process niceness as high as possible until sending rtp traffic
	err = service.SetNiceness(service.NicenessMax)
	if err != nil {
		log.Panic().
			Err(err).
			Msg("failed to set process niceness higher")
	}

	log.Info().
		Msg("starting listener")

	if err := server.ListenAndServe(ctx); err != nil {
		log.Panic().
			Err(err).
			Msg("server stopped unexpectedly")
	}

	log.Warn().
		Msg("server shutdown complete")
}
