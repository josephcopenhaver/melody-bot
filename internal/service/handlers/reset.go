package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Reset() HandleMessageCreate {

	return newHandleMessageCreate("reset", newWordMatcher(
		[]string{"reset"},
		func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

			p.Reset(m)

			return nil
		},
	))
}
