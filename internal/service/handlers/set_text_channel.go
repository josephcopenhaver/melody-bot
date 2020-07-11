package handlers

import (
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func SetTextChannel() HandleMessageCreate {

	return newHandleMessageCreate(
		"set-text-channel",
		"set text channel",
		"bot sends system text messages to the guild channel that this command is issued from",
		newRegexMatcher(
			true,
			regexp.MustCompile(`^\s*set\s*text\s*channel\s*$`),
			func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

				p.SetTextChannel(m.Message.ChannelID)

				return nil
			},
		),
	)
}
