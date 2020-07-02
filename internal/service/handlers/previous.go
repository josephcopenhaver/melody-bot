package handlers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Previous(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, handled *bool) error {

	if strings.TrimSpace(m.Message.Content) != "previous" && strings.TrimSpace(m.Message.Content) != "prev" {
		return nil
	}

	*handled = true

	p.Previous()

	return nil
}
