package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Previous() HandleMessageCreate {

	return newHandleMessageCreate(
		"previous",
		"<previous|prev>",
		"move playback to the previous track in the playlist",
		newWordMatcher(
			true,
			[]string{"previous", "prev"},
			func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

				p.Previous(m)

				return nil
			},
		),
	)
}
