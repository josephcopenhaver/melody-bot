package handlers

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Repeat() HandleMessageCreate {

	return newHandleMessageCreate("repeat", newWordMatcher(
		[]string{"repeat"},
		func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

			repeatMode := p.CycleRepeatMode(m)

			_, err := s.ChannelMessageSend(m.ChannelID, "repeat mode is now: "+repeatMode)
			return err
		},
	))
}
