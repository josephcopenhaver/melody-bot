package handlers

import (
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Echo() HandleMessageCreate {

	return newHandleMessageCreate("echo", newRegexMatcher(
		regexp.MustCompile(`^\s*echo\s+(?P<msg>[^\s]*.*?)\s*$`),
		func(s *discordgo.Session, m *discordgo.MessageCreate, _ *service.Player, args map[string]string) error {

			msg := args["msg"]
			if msg == "" {
				return nil
			}

			// log.Debug().
			// 	Str("payload", msg).
			// 	Msg("echo")

			_, err := s.ChannelMessageSend(m.ChannelID, msg)
			return err
		},
	))
}
