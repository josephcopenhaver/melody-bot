package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Reset() HandleMessageCreate {

	return newHandleMessageCreate(
		"reset",
		"reset",
		"resets player state back to defaults",
		newWordMatcher(
			true,
			[]string{"reset"},
			func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

				p.Reset(m)

				return nil
			},
		),
	)
}
