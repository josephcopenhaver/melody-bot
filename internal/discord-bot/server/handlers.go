package server

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/discord-bot/handlers"
	"github.com/rs/zerolog/log"
)

func (s *Server) Handlers() error {

	s.addMuxHandlers()

	s.AddHandler("ping", handlers.Ping)

	return nil
}

type HandleMessageCreate struct {
	Name    string
	Handler func(*discordgo.Session, *discordgo.MessageCreate, *bool) error
}

func newHandleMessageCreate(name string, handler func(*discordgo.Session, *discordgo.MessageCreate, *bool) error) HandleMessageCreate {
	return HandleMessageCreate{
		Name:    name,
		Handler: handler,
	}
}

func (srv *Server) addMuxHandlers() {
	srv.DiscordSession.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		var handled bool

		// ignore messages I (the bot) create
		if m.Author.ID == s.State.User.ID {
			return
		}

		for i := range srv.EventHandlers.MessageCreate {
			h := &srv.EventHandlers.MessageCreate[i]
			err := h.Handler(s, m, &handled)
			if err != nil {
				log.Error().
					Err(err).
					Str("handler_name", h.Name).
					Interface("author", m.Author).
					Interface("message", m.Message).
					Msg("error in handler")
			}

			if handled {
				break
			}
		}
	})
}

func (s *Server) AddHandler(name string, handler interface{}) {
	switch h := handler.(type) {
	case func(*discordgo.Session, *discordgo.MessageCreate, *bool) error:
		s.EventHandlers.MessageCreate = append(s.EventHandlers.MessageCreate, newHandleMessageCreate(name, h))
	default:
		log.Fatal().
			Str("handler_name", name).
			Msg("code-error: failed to register handler")
	}
}
