package handlers

import (
	"context"
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func RestartTrack() HandleMessageCreate {

	return newHandleMessageCreate(
		"restart-track",
		"restart track",
		"if playback is in the middle of a track, rewind to the start of the track",
		newRegexMatcher(
			true,
			regexp.MustCompile(`^\s*restart\s+track\s*$`),
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

				p.RestartTrack(m)

				return nil
			},
		),
	)
}
