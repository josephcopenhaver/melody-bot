package handlers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Stop(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, handled *bool) error {

	if strings.TrimSpace(m.Message.Content) != "stop" {
		return nil
	}

	*handled = true

	p.Stop()

	return nil
}
