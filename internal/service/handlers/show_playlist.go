package handlers

import (
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func ShowPlaylist() HandleMessageCreate {

	return newHandleMessageCreate(
		"show-playlist",
		"show playlist",
		"prints the current playlist",
		newRegexMatcher(
			true,
			regexp.MustCompile(`^\s*show\s*playlist\s*$`),
			func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ map[string]string) error {

				playlist := p.GetPlaylist()

				if len(playlist.Tracks) == 0 {
					_, err := s.ChannelMessageSend(m.Message.ChannelID, "# no tracks in playlist")
					return err
				}

				msg := "---\n#\n# playlist:\n#\n"

				for i, t := range playlist.Tracks {
					msg += "\n- url: `" + t.Url + "`\n" +
						"  from: " + t.AuthorMention + "\n"
					if i == playlist.CurrentTrackIdx {
						msg += "  state: playing\n"
					} else {
						msg += "  state: queued\n"
					}
				}

				_, err := s.ChannelMessageSend(m.Message.ChannelID, msg)
				return err
			},
		),
	)
}
