package handlers

import (
	"context"
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func ClearPlaylist() HandleMessageCreate {

	return newHandleMessageCreate(
		"clear-playlist",
		"clear playlist",
		"removes all tracks in the playlist",
		newRegexMatcher(
			true,
			regexp.MustCompile(`^\s*clear\s+playlist\s*$`),
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

				p.ClearPlaylist(m)

				return nil
			},
		),
	)
}
