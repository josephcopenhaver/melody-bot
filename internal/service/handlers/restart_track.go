package handlers

import (
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func RestartTrack() HandleMessageCreate {

	return newHandleMessageCreate("restart-track", newRegexMatcher(
		regexp.MustCompile(`^\s*restart\s+track\s*$`),
		func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

			p.RestartTrack(m)

			return nil
		},
	))
}
