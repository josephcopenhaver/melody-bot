package handlers

import (
	"github.com/bwmarrin/discordgo"
)

func Ping() (string, []string, func(*discordgo.Session, *discordgo.MessageCreate) error) {

	n := "ping"
	m := []string{"ping"}
	h := func(s *discordgo.Session, m *discordgo.MessageCreate) error {

		_, err := s.ChannelMessageSend(m.ChannelID, "pong, in reply to "+m.Message.Author.Mention())
		return err
	}

	return n, m, h
}
