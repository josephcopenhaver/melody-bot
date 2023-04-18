package handlers

import (
	"context"
	"sort"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Help(inputHandlers []HandleMessageCreate) HandleMessageCreate {

	msg := "```\n---\n#\n# help:\n#\n"

	result := newHandleMessageCreate(
		"help",
		"help",
		"enumerates each bot command, it's syntax, and what the command does",
		newWordMatcher(
			false,
			[]string{"help"},
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, _ *service.Player) error {

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

	handlers := make([]HandleMessageCreate, len(inputHandlers), len(inputHandlers)+1)
	copy(handlers, inputHandlers)
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

	msg += "```"

	return result
}
