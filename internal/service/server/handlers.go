package server

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
	"github.com/josephcopenhaver/discord-bot/internal/service/handlers"
	"github.com/rs/zerolog/log"
)

func (s *Server) Handlers() error {

	// https://discord.com/developers/docs/topics/gateway#event-names

	s.addMuxHandlers()

	s.AddHandler(handlers.Ping())

	s.AddHandler(handlers.JoinChannel())

	s.AddHandler(handlers.Reset())

	s.AddHandler(handlers.Play())

	s.AddHandler(handlers.Resume()) // also alias for play ( without args )

	s.AddHandler(handlers.Pause())

	s.AddHandler(handlers.Stop())

	s.AddHandler(handlers.Repeat())

	s.AddHandler(handlers.Next()) // also alias for skip

	s.AddHandler(handlers.Previous()) // also alias for prev

	s.AddHandler(handlers.RestartTrack())

	s.AddHandler(handlers.ClearPlaylist())

	s.AddHandler(handlers.Echo())

	s.AddHandler(handlers.SetTextChannel())

	s.DiscordSession.AddHandler(func(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
		// https://discord.com/developers/docs/topics/gateway#voice-state-update
		// Sent when someone joins/leaves/moves voice channels. Inner payload is a voice state object.
		log.Debug().
			Interface("payload", v).
			Msg("event: voice state update")

		// intent: when the bot is forced to change channels, may want to renew the brodcast channel
		// intent: when current channel becomes empty, ensure playback is paused or stopped
	})

	s.DiscordSession.AddHandler(func(s *discordgo.Session, v *discordgo.GuildDelete) {
		// https://discord.com/developers/docs/topics/gateway#guild-delete
		// Sent when a guild becomes unavailable during a guild outage, or when the user leaves or is removed from a guild. The inner payload is an unavailable guild object. If the unavailable field is not set, the user was removed from the guild.
		log.Debug().
			Interface("payload", v).
			Msg("event: guild delete")

		// intent: delete any active player ( stop broadcast goroutine ) when bot is kicked from a server
	})

	s.DiscordSession.AddHandler(func(s *discordgo.Session, v *discordgo.ChannelDelete) {
		// https://discord.com/developers/docs/topics/gateway#channel-delete
		// Sent when a channel relevant to the current user is deleted. The inner payload is a channel object.
		log.Debug().
			Interface("payload", v).
			Msg("event: channel delete")

		// intent: pause any active broadcast when bot is kicked from a channel
	})

	// always keep last, it analyzes registered handlers
	s.AddHandler(handlers.Help(s.EventHandlers.MessageCreate))

	return nil
}

func (srv *Server) addMuxHandlers() {
	srv.DiscordSession.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		var err error
		var p *service.Player

		// ignore messages I (the bot) create
		if m.Author.ID == s.State.User.ID {
			return
		}

		trimMsg := strings.TrimSpace(m.Message.Content)

		if m.GuildID != "" {

			p = srv.Brain.Player(s, m.GuildID)

			// verify the user is giving me a direct command in a guild channel
			// if so then run handlers

			if !strings.HasPrefix(trimMsg, "<@") {
				return
			}

			prefix := func() string {

				prefix := s.State.User.Mention()
				if strings.HasPrefix(trimMsg, prefix) {
					return prefix
				}

				// log.Debug().
				// 	Str("channel_message", trimMsg).
				// 	Str("prefix", prefix).
				// 	Str("mention", "user").
				// 	Msg("no match")

				member, err := s.State.Member(m.GuildID, s.State.User.ID)
				if err != nil {
					log.Err(err).
						Msg("failed to get my own member status")
					return ""
				}

				prefix = member.Mention()
				if strings.HasPrefix(trimMsg, prefix) {
					return prefix
				}

				// log.Debug().
				// 	Str("channel_message", trimMsg).
				// 	Str("prefix", prefix).
				// 	Str("mention", "member").
				// 	Msg("no match")

				for _, roleId := range member.Roles {

					r, err := s.State.Role(m.GuildID, roleId)
					if err != nil {
						log.Err(err).
							Str("role_id", roleId).
							Msg("failed to get role info")
						continue
					}

					if r.Name != s.State.User.Username {
						// log.Debug().
						// 	Str("role_name", r.Name).
						// 	Str("user_name", s.State.User.Username).
						// 	Msg("role name does not match")
						continue
					}

					prefix = r.Mention()
					if strings.HasPrefix(trimMsg, prefix) {
						return prefix
					}

					// log.Debug().
					// 	Str("channel_message", trimMsg).
					// 	Str("prefix", prefix).
					// 	Str("mention", "role").
					// 	Msg("no match")
				}

				return ""
			}()

			if prefix == "" {
				// log.Debug().
				// 	Str("channel_message", trimMsg).
				// 	Msg("message not for me")
				return
			}

			// get message without @bot directive
			{
				withoutMetion := trimMsg[len(prefix):]
				newTrimMsg := strings.TrimSpace(withoutMetion)
				if newTrimMsg == withoutMetion {
					// log.Debug().
					// 	Str("channel_message", trimMsg).
					// 	Msg("not well formed for me")
					return
				}

				trimMsg = newTrimMsg
			}
		}

		for i := range srv.EventHandlers.MessageCreate {

			h := &srv.EventHandlers.MessageCreate[i]

			handler := h.Matcher(p, trimMsg)
			if handler == nil {
				continue
			}

			err := handler(s, m, p)
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

			// log.Info().
			// 	Str("handler_name", h.Name).
			// 	Str("author_id", m.Author.ID).
			// 	Str("author_username", m.Author.Username).
			// 	Str("message_content", m.Message.Content).
			// 	Interface("message_id", m.Message.ID).
			// 	Interface("message_timestamp", m.Message.Timestamp).
			// 	Msg("handled message")
			return
		}

		// log.Debug().
		// 	Str("author_id", m.Author.ID).
		// 	Str("author_username", m.Author.Username).
		// 	Str("message_content", m.Message.Content).
		// 	Interface("message_id", m.Message.ID).
		// 	Interface("message_timestamp", m.Message.Timestamp).
		// 	Msg("unhandled message")

		_, err = s.ChannelMessageSend(m.ChannelID, "command not recognized")
		if err != nil {
			log.Error().
				Err(err).
				Msg("failed to send default reply")
		}
	})
}

func (s *Server) AddHandler(v interface{}) {

	switch h := v.(type) {

	case handlers.HandleMessageCreate:
		s.EventHandlers.MessageCreate = append(s.EventHandlers.MessageCreate, h)

	default:
		log.Fatal().
			Interface("handler", v).
			Msg("code-error: failed to register handler")
	}
}
