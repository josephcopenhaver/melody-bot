package handlers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

func Ping(s *discordgo.Session, m *discordgo.MessageCreate, handled *bool) error {

	if strings.TrimSpace(m.Content) != "ping" {
		return nil
	}

	*handled = true

	_, err := s.ChannelMessageSend(m.ChannelID, "pong")

	return err
}
