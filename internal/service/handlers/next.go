package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Next() HandleMessageCreate {

	return newHandleMessageCreate(
		"next",
		"<next|skip>",
		"move playback to the next track in the playlist",
		newWordMatcher(
			[]string{"next", "skip"},
			func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

				p.Next(m)

				return nil
			},
		),
	)
}
