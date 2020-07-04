package handlers

import (
	"github.com/bwmarrin/discordgo"
)

func Help() (string, []string, func(*discordgo.Session, *discordgo.MessageCreate) error) {

	n := "help"
	m := []string{"help"}
	h := func(s *discordgo.Session, m *discordgo.MessageCreate) error {

		// _, err := s.ChannelMessageSend(m.ChannelID, "pong, in reply to "+m.Message.Author.Mention())

		// TODO: for each registered handler, enumerate how to invoke and what the invocation does
		// s := "Cmd: "s.State.User.Mention() + " cmd|alias args..."
		// s += "\n does X, Y, and Z"
		// s += "\n"

		return nil
	}

	return n, m, h
}
