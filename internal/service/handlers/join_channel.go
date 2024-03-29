package handlers

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

// TODO: handle when a sysadmin moves the bot to another channel manually

func JoinChannel() HandleMessageCreate {

	return newHandleMessageCreate(
		"join-channel",
		"join <channel_name>",
		"makes the bot join a specific voice channel",
		newRegexMatcher(
			true,
			regexp.MustCompile(`^\s*join\s+(?P<channel_name>[^\s]+.*?)\s*$`),
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, args map[string]string) error {

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
					deaf := true

					vc, err := s.ChannelVoiceJoin(c.GuildID, c.ID, mute, deaf)
					if err != nil {
						return err
					}

					p.SetVoiceConnection(m, c.ID, vc)
					return nil
				}

				return errors.New("could not find channel")
			},
		),
	)
}
