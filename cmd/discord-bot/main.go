package main

import (
	"os"

	"github.com/josephcopenhaver/discord-bot/internal/discord-bot/config"
	"github.com/josephcopenhaver/discord-bot/internal/discord-bot/logging"
	"github.com/josephcopenhaver/discord-bot/internal/discord-bot/server"
	"github.com/josephcopenhaver/discord-bot/internal/discord-bot/serviceinfo"
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

	serviceinfo.Version = Version
	serviceinfo.Commit = GitSHA
	serviceinfo.StartupMessage()

	conf, err := config.NewConfig()
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

	if err := server.ListenAndServe(); err != nil {
		log.Fatal().
			Err(err).
			Msg("server stopped unexpectedly")
	}

	log.Warn().
		Msg("server shutdown complete")
}
