package handlers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Resume(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, handled *bool) error {

	if strings.TrimSpace(m.Message.Content) != "resume" && strings.TrimSpace(m.Message.Content) != "play" {
		return nil
	}

	*handled = true

	p.Resume()

	return nil
}
