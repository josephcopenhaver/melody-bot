package main

import (
	"os"

	"github.com/josephcopenhaver/discord-bot/internal/logging"
	"github.com/josephcopenhaver/discord-bot/internal/service"
	"github.com/josephcopenhaver/discord-bot/internal/service/config"
	"github.com/josephcopenhaver/discord-bot/internal/service/server"
	"github.com/rs/zerolog/log"
)

var GitSHA string
var Version string

func main() {
	logLevelStr := os.Getenv("LOG_LEVEL")
	if logLevelStr != "" {
		if err := logging.SetGlobalLevel(logLevelStr); err != nil {
			log.Fatal().
				Str("LOG_LEVEL", logLevelStr).
				Msg("invalid log level")
		}
	}

	service.Version = Version
	service.Commit = GitSHA
	log.Info().
		Str("Version", service.Version).
		Str("Commit", service.Commit).
		Msg("starting discord-bot")

	conf, err := config.New()
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("failed to read configuration")
	}

	server := server.New()
	if err := server.SetConfig(conf); err != nil {
		log.Fatal().
			Err(err).
			Msg("failed to load configuration")
	}

	if err := server.Handlers(); err != nil {
		log.Fatal().
			Err(err).
			Msg("failed to register handlers")
	}

	service.NicenessInit()

	// set process niceness as high as possible until sending rtp traffic
	err = service.SetNiceness(service.NicenessMax)
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("failed to set process niceness higher")
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatal().
			Err(err).
			Msg("server stopped unexpectedly")
	}

	log.Warn().
		Msg("server shutdown complete")
}
