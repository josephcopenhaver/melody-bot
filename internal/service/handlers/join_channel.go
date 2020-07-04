package handlers

import (
	"errors"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

// TODO: handle when a sysadmin moves the bot to another channel manually

func JoinChannel() HandleMessageCreate {

	return newHandleMessageCreate("join-channel", newRegexMatcher(
		regexp.MustCompile(`^\s*join\s+(?P<channel_name>[^\s]+.*?)\s*$`),
		func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, args map[string]string) error {

			channelName := args["channel_name"]

			if channelName == "" {
				return nil
			}

			channels, err := s.GuildChannels(m.Message.GuildID)
			if err != nil {
				return err
			}

			for _, c := range channels {

				if c.Type != discordgo.ChannelTypeGuildVoice || strings.TrimSpace(c.Name) != channelName {
					continue
				}

				mute := false
				deaf := false

				vc, err := s.ChannelVoiceJoin(c.GuildID, c.ID, mute, deaf)
				if err != nil {
					return err
				}

				p.SetVoiceConnection(m, vc)
				return nil
			}

			return errors.New("could not find channel")
		},
	))
}
