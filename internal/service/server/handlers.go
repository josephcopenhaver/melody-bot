package server

import (
	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
	"github.com/josephcopenhaver/discord-bot/internal/service/handlers"
	"github.com/rs/zerolog/log"
)

func (s *Server) Handlers() error {

	// https://discord.com/developers/docs/topics/gateway#event-names

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

	s.AddHandler("clear-playlist", handlers.ClearPlaylist)

	s.DiscordSession.AddHandler(func(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
		// https://discord.com/developers/docs/topics/gateway#voice-state-update
		// Sent when someone joins/leaves/moves voice channels. Inner payload is a voice state object.
		log.Warn().
			Interface("payload", v).
			Msg("event: voice state update")

		// intent: when the bot is forced to change channels, may want to renew the brodcast channel
		// intent: when current channel becomes empty, ensure playback is paused or stopped
	})

	s.DiscordSession.AddHandler(func(s *discordgo.Session, v *discordgo.GuildDelete) {
		// https://discord.com/developers/docs/topics/gateway#guild-delete
		// Sent when a guild becomes unavailable during a guild outage, or when the user leaves or is removed from a guild. The inner payload is an unavailable guild object. If the unavailable field is not set, the user was removed from the guild.
		log.Warn().
			Interface("payload", v).
			Msg("event: guild delete")

		// intent: delete any active player ( stop broadcast goroutine ) when bot is kicked from a server
	})

	s.DiscordSession.AddHandler(func(s *discordgo.Session, v *discordgo.ChannelDelete) {
		// https://discord.com/developers/docs/topics/gateway#channel-delete
		// Sent when a channel relevant to the current user is deleted. The inner payload is a channel object.
		log.Warn().
			Interface("payload", v).
			Msg("event: channel delete")

		// intent: pause any active broadcast when bot is kicked from a channel
	})

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
		var p *service.Player
		var handled bool

		// ignore messages I (the bot) create
		if m.Author.ID == s.State.User.ID {
			return
		}

		if m.GuildID != "" {
			p = srv.Brain.Player(m.GuildID)
		}

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
