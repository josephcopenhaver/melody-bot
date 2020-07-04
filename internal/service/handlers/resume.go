package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Resume() (string, []string, func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error) {

	n := "resume"
	m := []string{"resume"}
	h := func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

		p.Resume()

		return nil
	}

	return n, m, h
}
