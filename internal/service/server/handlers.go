package server

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
	"github.com/josephcopenhaver/discord-bot/internal/service/handlers"
	"github.com/rs/zerolog/log"
)

func (s *Server) Handlers() error {

	s.addMuxHandlers()

	s.AddHandler("ping", handlers.Ping)

	s.AddHandler("join-channel", handlers.JoinChannel)

	s.AddHandler("reset", handlers.Reset)

	s.AddHandler("play", handlers.Play)

	s.AddHandler("resume", handlers.Resume) // also alias for play ( without args )

	s.AddHandler("pause", handlers.Pause)

	s.AddHandler("stop", handlers.Stop)

	s.AddHandler("repeat", handlers.Repeat)

	s.AddHandler("next", handlers.Next) // also alias for skip

	s.AddHandler("previous", handlers.Previous) // also alias for prev

	s.AddHandler("restart-track", handlers.RestartTrack)

	return nil
}

type HandleMessageCreate struct {
	Name    string
	Handler func(*discordgo.Session, *discordgo.MessageCreate, *service.Player, *bool) error
}

func newHandleMessageCreate(name string, handler func(*discordgo.Session, *discordgo.MessageCreate, *service.Player, *bool) error) HandleMessageCreate {
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

		p := srv.Brain.Player(m.GuildID)

		for i := range srv.EventHandlers.MessageCreate {

			h := &srv.EventHandlers.MessageCreate[i]

			err := h.Handler(s, m, p, &handled)
			if err != nil {
				log.Err(err).
					Str("handler_name", h.Name).
					Str("author_id", m.Author.ID).
					Str("author_username", m.Author.Username).
					Str("message_content", m.Message.Content).
					Interface("message_id", m.Message.ID).
					Interface("message_timestamp", m.Message.Timestamp).
					Msg("error in handler")

				_, err := s.ChannelMessageSend(m.ChannelID, "error: "+err.Error())
				if err != nil {
					log.Err(err).
						Msg("failed to send error reply")
				}
				return
			}

			if handled {
				log.Warn().
					Str("handler_name", h.Name).
					Str("author_id", m.Author.ID).
					Str("author_username", m.Author.Username).
					Str("message_content", m.Message.Content).
					Interface("message_id", m.Message.ID).
					Interface("message_timestamp", m.Message.Timestamp).
					Msg("handled message")
				return
			}
		}

		log.Info().
			Str("author_id", m.Author.ID).
			Str("author_username", m.Author.Username).
			Str("message_content", m.Message.Content).
			Interface("message_id", m.Message.ID).
			Interface("message_timestamp", m.Message.Timestamp).
			Msg("unhandled message")

		_, err := s.ChannelMessageSend(m.ChannelID, "command not recognized")
		if err != nil {
			log.Error().
				Err(err).
				Msg("failed to send default reply")
		}
	})
}

func (s *Server) AddHandler(name string, handler interface{}) {
	switch h := handler.(type) {
	case func(*discordgo.Session, *discordgo.MessageCreate, *bool) error:
		w := func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, handled *bool) error {
			return h(s, m, handled)
		}
		s.EventHandlers.MessageCreate = append(s.EventHandlers.MessageCreate, newHandleMessageCreate(name, w))
	case func(*discordgo.Session, *discordgo.MessageCreate, *service.Player, *bool) error:
		s.EventHandlers.MessageCreate = append(s.EventHandlers.MessageCreate, newHandleMessageCreate(name, h))
	default:
		log.Fatal().
			Str("handler_name", name).
			Msg("code-error: failed to register handler")
	}
}
