package handlers

import (
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

var reRestartTrack = regexp.MustCompile(`^\s*restart\s+track\s*$`)

func RestartTrack(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, handled *bool) error {

	args := regexMap(reRestartTrack, m.Message.Content)
	if args == nil {
		return nil
	}

	*handled = true

	p.RestartTrack()

	return nil
}
