package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Resume() HandleMessageCreate {

	return newHandleMessageCreate(
		"resume",
		"<resume|play>",
		"if stopped or paused, resumes playback",
		newWordMatcher(
			true,
			[]string{"resume", "play"},
			func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

				p.Resume(m)

				return nil
			},
		),
	)
}
