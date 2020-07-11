package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Stop() HandleMessageCreate {

	return newHandleMessageCreate(
		"stop",
		"stop",
		"stops playback of current track and rewinds to the beginning of the current track",
		newWordMatcher(
			true,
			[]string{"stop"},
			func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

				p.Stop(m)

				return nil
			},
		),
	)
}
