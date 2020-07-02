package handlers

import (
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

var reClearPlaylist = regexp.MustCompile(`^\s*clear\s+playlist\s*$`)

// TODO: handle when a sysadmin moves the bot to another channel manually

func ClearPlaylist(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, handled *bool) error {

	args := regexMap(reClearPlaylist, m.Message.Content)
	if args == nil {
		return nil
	}

	*handled = true

	p.ClearPlaylist()

	return nil
}
