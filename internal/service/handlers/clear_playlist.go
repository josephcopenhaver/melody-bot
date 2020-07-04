package handlers

import (
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func ClearPlaylist() HandleMessageCreate {

	return newHandleMessageCreate("clear-playlist", newRegexMatcher(
		regexp.MustCompile(`^\s*clear\s+playlist\s*$`),
		func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

			p.ClearPlaylist(m)

			return nil
		},
	))
}
