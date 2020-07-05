package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Ping() HandleMessageCreate {

	return newHandleMessageCreate(
		"ping",
		"ping",
		"responds with pong message",
		newWordMatcher(
			[]string{"ping"},
			func(s *discordgo.Session, m *discordgo.MessageCreate, _ *service.Player, _ map[string]string) error {

				_, err := s.ChannelMessageSend(m.ChannelID, "pong, in reply to "+m.Message.Author.Mention())
				return err
			},
		),
	)
}
