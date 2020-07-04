package handlers

import (
	"regexp"

	"github.com/rs/zerolog/log"

	"github.com/bwmarrin/discordgo"
)

func Echo() (string, *regexp.Regexp, func(*discordgo.Session, *discordgo.MessageCreate, map[string]string) error) {

	n := "echo"
	m := regexp.MustCompile(`^\s*echo\s+(?P<msg>[^\s]*?)\s*$`)
	h := func(s *discordgo.Session, m *discordgo.MessageCreate, args map[string]string) error {

		msg := args["msg"]
		if msg == "" {
			return nil
		}

		log.Info().
			Str("payload", msg).
			Msg("echo")

		_, err := s.ChannelMessageSend(m.ChannelID, msg)
		return err
	}

	return n, m, h
}
