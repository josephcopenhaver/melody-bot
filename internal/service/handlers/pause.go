package handlers

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Pause() HandleMessageCreate {

	return newHandleMessageCreate(
		"pause",
		"pause",
		"pauses playback and remember position in the current track; can be resumed",
		newWordMatcher(
			true,
			[]string{"pause"},
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

				p.Pause(m)

				return nil
			},
		),
	)
}
