package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Pause() (string, []string, func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error) {

	n := "pause"
	m := []string{"pause"}
	h := func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

		p.Pause()

		return nil
	}

	return n, m, h
}
