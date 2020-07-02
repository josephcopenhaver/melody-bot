package handlers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Next(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, handled *bool) error {

	if strings.TrimSpace(m.Message.Content) != "next" && strings.TrimSpace(m.Message.Content) != "skip" {
		return nil
	}

	*handled = true

	p.Next()

	return nil
}
