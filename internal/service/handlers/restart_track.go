package handlers

import (
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func RestartTrack() (string, *regexp.Regexp, func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error) {

	n := "restart-track"
	m := regexp.MustCompile(`^\s*restart\s+track\s*$`)
	h := func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

		p.RestartTrack()

		return nil
	}

	return n, m, h
}
