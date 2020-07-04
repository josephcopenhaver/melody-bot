package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Next() (string, []string, func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error) {

	n := "next"
	m := []string{"next", "skip"}
	h := func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

		p.Next()

		return nil
	}

	return n, m, h
}
