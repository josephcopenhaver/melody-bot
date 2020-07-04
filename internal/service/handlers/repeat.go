package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Repeat() (string, []string, func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error) {

	n := "repeat"
	m := []string{"repeat"}
	h := func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {

		repeatMode := p.CycleRepeatMode()

		_, err := s.ChannelMessageSend(m.ChannelID, "repeat mode is now: "+repeatMode)
		return err
	}

	return n, m, h
}
