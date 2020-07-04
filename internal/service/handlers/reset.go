package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Reset() (string, []string, func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error) {

	n := "reset"
	m := []string{"reset"}
	h := func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

		p.Reset()

		return nil
	}

	return n, m, h
}
