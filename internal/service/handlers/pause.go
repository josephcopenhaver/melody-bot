package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Pause() HandleMessageCreate {

	return newHandleMessageCreate("pause", newWordMatcher(
		[]string{"pause"},
		func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

			p.Pause(m)

			return nil
		},
	))
}
