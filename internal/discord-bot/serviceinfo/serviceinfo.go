package serviceinfo

import (
	"github.com/rs/zerolog/log"
)

var Version string
var Commit string

func StartupMessage() {
	log.Info().
		Str("Version", Version).
		Str("Commit", Commit).
		Msg("starting discord-bot")
}
