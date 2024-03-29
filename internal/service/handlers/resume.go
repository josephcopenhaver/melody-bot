package handlers

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Resume() HandleMessageCreate {

	return newHandleMessageCreate(
		"resume",
		"<resume|unpause|play>",
		"if stopped or paused, resumes playback",
		newWordMatcher(
			true,
			[]string{"resume", "unpause", "play"},
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

				p.Resume(m)

				return nil
			},
		),
	)
}
