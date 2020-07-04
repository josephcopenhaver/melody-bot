package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Stop() HandleMessageCreate {

	return newHandleMessageCreate("stop", newWordMatcher(
		[]string{"stop"},
		func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

			p.Stop(m)

			return nil
		},
	))
}
