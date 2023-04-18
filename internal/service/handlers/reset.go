package handlers

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Reset() HandleMessageCreate {

	return newHandleMessageCreate(
		"reset",
		"reset",
		"resets player state back to defaults: stops playback and clears the playlist",
		newWordMatcher(
			true,
			[]string{"reset"},
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

				p.Reset(m)

				return nil
			},
		),
	)
}
