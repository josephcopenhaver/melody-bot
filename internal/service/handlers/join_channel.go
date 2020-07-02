package handlers

import (
	"errors"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

var reJoinChannel = regexp.MustCompile(`^\s*join\s+(?P<channel_name>[^\s]+.*?)\s*$`)

// TODO: handle when a sysadmin moves the bot to anothe channel manually

func JoinChannel(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, handled *bool) error {

	args := regexMap(reJoinChannel, m.Message.Content)
	if args == nil {
		return nil
	}

	channelName := args["channel_name"]

	if channelName == "" {
		return nil
	}

	*handled = true

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

		p.SetVoiceConnection(vc)
		return nil
	}

	return errors.New("could not find channel")
}
