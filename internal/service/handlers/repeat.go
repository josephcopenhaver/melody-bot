package handlers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Repeat(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, handled *bool) error {

	if strings.TrimSpace(m.Message.Content) != "repeat" {
		return nil
	}

	*handled = true

	repeatMode := p.CycleRepeatMode()

	_, err := s.ChannelMessageSend(m.ChannelID, "repeat mode is now: "+repeatMode)
	return err
}
