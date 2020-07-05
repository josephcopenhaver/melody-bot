package handlers

import (
	"sort"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

func Help(handlers []HandleMessageCreate) HandleMessageCreate {

	msg := "---"

	result := newHandleMessageCreate(
		"help",
		"help",
		"enumerates each bot command, it's syntax, and what the command does",
		newWordMatcher(
			[]string{"help"},
			func(s *discordgo.Session, m *discordgo.MessageCreate, _ *service.Player, _ map[string]string) error {

				userTxtChan, err := s.UserChannelCreate(m.Author.ID)
				if err != nil {
					return err
				}

				_, err = s.ChannelMessageSend(userTxtChan.ID, msg)
				if err != nil {
					return err
				}

				return nil
			},
		),
	)

	handlers = append(handlers, result)

	sort.Slice(handlers, func(i, j int) bool {

		return handlers[i].Name < handlers[j].Name
	})

	for _, h := range handlers {

		msg += "\n" + h.Name + ":\n"

		if h.Usage != "" {
			msg += "  usage: " + h.Usage + "\n"
		}

		if h.Description != "" {
			msg += "  description: " + h.Description + "\n"
		}

	}

	return result
}
