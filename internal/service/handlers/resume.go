package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Resume() HandleMessageCreate {

	return newHandleMessageCreate("resume", newWordMatcher(
		[]string{"resume"},
		func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

			p.Resume(m)

			return nil
		},
	))
}
