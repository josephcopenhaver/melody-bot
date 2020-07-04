package handlers

import (
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func ClearPlaylist() (string, *regexp.Regexp, func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error) {

	n := "clear-playlist"
	m := regexp.MustCompile(`^\s*clear\s+playlist\s*$`)
	h := func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

		p.ClearPlaylist()

		return nil
	}

	return n, m, h
}
