package handlers

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Next() HandleMessageCreate {

	return newHandleMessageCreate(
		"next",
		"<next|skip>",
		"move playback to the next track in the playlist",
		newWordMatcher(
			true,
			[]string{"next", "skip"},
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

				p.Next(m)

				return nil
			},
		),
	)
}
