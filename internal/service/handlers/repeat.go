package handlers

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Repeat() HandleMessageCreate {

	return newHandleMessageCreate(
		"repeat",
		"repeat",
		"cycles playlist repeat mode between [\"repeating\", \"not repeating\"]",
		newWordMatcher(
			true,
			[]string{"repeat"},
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

				repeatMode := p.CycleRepeatMode(m)

				_, err := s.ChannelMessageSend(m.ChannelID, "repeat mode is now: "+repeatMode)
				return err
			},
		),
	)
}
