package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Previous() (string, []string, func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error) {

	n := "previous"
	m := []string{"previous", "prev"}
	h := func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

		p.Previous()

		return nil
	}

	return n, m, h
}
