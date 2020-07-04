package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Help() HandleMessageCreate {

	return newHandleMessageCreate("help", newWordMatcher(
		[]string{"help"},
		func(s *discordgo.Session, m *discordgo.MessageCreate, _ *service.Player, _ map[string]string) error {

			// _, err := s.ChannelMessageSend(m.ChannelID, "pong, in reply to "+m.Message.Author.Mention())

			// TODO: for each registered handler, enumerate how to invoke and what the invocation does
			// s := "Cmd: "s.State.User.Mention() + " cmd|alias args..."
			// s += "\n does X, Y, and Z"
			// s += "\n"

			return nil
		},
	))
}
