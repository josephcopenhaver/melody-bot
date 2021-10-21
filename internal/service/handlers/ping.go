package handlers

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Ping() HandleMessageCreate {

	return newHandleMessageCreate(
		"ping",
		"ping",
		"responds with pong message",
		newWordMatcher(
			false,
			[]string{"ping"},
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, _ *service.Player, _ map[string]string) error {

				_, err := s.ChannelMessageSend(m.ChannelID, "pong")
				return err
			},
		),
	)
}
