package handlers

import (
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func RemoveTrack() HandleMessageCreate {

	return newHandleMessageCreate(
		"remove-track",
		"remove <track_url>",
		"removes a track from the playlist",
		newRegexMatcher(
			true,
			regexp.MustCompile(`^\s*remove\s+(?P<track_url>[^\s]+.*?)\s*$`),
			func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, args map[string]string) error {

				url := args["track_url"]

				if url == "" {
					return nil
				}

				removed := p.RemoveTrack(url)

				var msg string
				if removed {
					msg += "track removed: `" + url + "`"
				} else {
					msg += "track not found: `" + url + "`"
				}

				_, err := s.ChannelMessageSend(m.Message.ChannelID, msg)
				return err
			},
		),
	)
}
