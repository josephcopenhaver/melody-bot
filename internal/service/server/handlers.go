package server

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
	"github.com/josephcopenhaver/melody-bot/internal/service/handlers"
	. "github.com/josephcopenhaver/melody-bot/internal/service/server/reactions"
	"github.com/rs/zerolog"
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

	s.AddHandler(handlers.ShowPlaylist())

	s.AddHandler(handlers.RemoveTrack())

	s.AddHandler(handlers.ClearCache())

	s.DiscordSession.AddHandler(func(session *discordgo.Session, evt *discordgo.VoiceStateUpdate) {
		// https://discord.com/developers/docs/topics/gateway#voice-state-update
		// Sent when someone joins/leaves/moves voice channels. Inner payload is a voice state object.

		if s.ctx.Err() != nil {
			return
		}

		log.Debug().
			Msg("evt: join/leave/move voice channel")

		// if not a destructive event then short circuit
		if evt.VoiceState == nil || evt.VoiceState.GuildID == "" || evt.VoiceState.ChannelID == "" {
			return
		}

		// if the event spawner is the bot, short circuit
		if evt.Member != nil && evt.Member.User != nil && evt.Member.User.ID == s.DiscordSession.State.User.ID {
			return
		}

		// if player does not exist then short circuit
		if !s.Brain.PlayerExists(session, evt.VoiceState.GuildID) {
			return
		}

		p := s.Brain.Player(s.ctx, &s.wg, session, evt.VoiceState.GuildID)
		if p.HasAudience() {
			return
		}

		log.Debug().
			Msg("no audience, pausing")

		p.Pause(evt)
	})

	s.DiscordSession.AddHandler(func(session *discordgo.Session, evt *discordgo.GuildDelete) {
		// https://discord.com/developers/docs/topics/gateway#guild-delete
		// Sent when a guild becomes unavailable during a guild outage, or when the user leaves or is removed from a guild. The inner payload is an unavailable guild object. If the unavailable field is not set, the user was removed from the guild.

		if s.ctx.Err() != nil {
			return
		}

		log.Debug().
			Msg("evt: guild unavailable")

		if evt.ID == "" || evt.Unavailable {
			return
		}

		// if player does not exist then short circuit
		if !s.Brain.PlayerExists(session, evt.ID) {
			return
		}

		p := s.Brain.Player(s.ctx, &s.wg, session, evt.ID)

		channelId := p.GetVoiceChannelId()
		if channelId == "" {
			return
		}

		// TODO: delete player instead of stop

		p.Stop(evt)
	})

	s.DiscordSession.AddHandler(func(session *discordgo.Session, evt *discordgo.ChannelDelete) {
		// https://discord.com/developers/docs/topics/gateway#channel-delete
		// Sent when a channel relevant to the current user is deleted. The inner payload is a channel object.

		if s.ctx.Err() != nil {
			return
		}

		log.Debug().
			Msg("evt: channel deleted")

		if evt.Channel == nil || evt.Channel.GuildID == "" {
			return
		}

		// if player does not exist then short circuit
		if !s.Brain.PlayerExists(session, evt.Channel.GuildID) {
			return
		}

		p := s.Brain.Player(s.ctx, &s.wg, session, evt.Channel.GuildID)

		channelId := p.GetVoiceChannelId()
		if channelId == "" {
			return
		}

		if evt.Channel.ID != channelId {
			return
		}

		p.Pause(evt)
		p.SetVoiceConnection(evt, "", nil)
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

		if srv.ctx.Err() != nil {
			return
		}

		trimMsg := strings.TrimSpace(m.Message.Content)

		if m.GuildID != "" {

			p = srv.Brain.Player(srv.ctx, &srv.wg, s, m.GuildID)

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
				withoutMention := trimMsg[len(prefix):]
				newTrimMsg := strings.TrimSpace(withoutMention)
				if newTrimMsg == withoutMention {
					// log.Debug().
					// 	Str("channel_message", trimMsg).
					// 	Msg("not well formed for me")
					return
				}

				trimMsg = newTrimMsg
			}
		}

		logger := log.Level(zerolog.DebugLevel)
		{
			lc := logger.With().
				Str("guild_id", m.GuildID).
				Str("channel_id", m.ChannelID).
				Str("message_id", m.Message.ID).
				Time("message_timestamp", m.Timestamp).
				Str("author_id", m.Author.ID).
				Str("author_username", m.Author.Username)

			if m.EditedTimestamp != nil {
				lc = lc.Time("last_edited_at", *m.EditedTimestamp)
			}

			logger = lc.Logger()
		}

		ctx := logger.WithContext(srv.ctx)

		err = s.MessageReactionAdd(m.ChannelID, m.ID, ReactionStatusThinking.String())
		if err != nil {
			logger.Err(err).Msg("failed to react with thinking")
		} else {
			defer func() {
				err := s.MessageReactionRemove(m.ChannelID, m.ID, ReactionStatusThinking.String(), "@me")
				if err != nil {
					logger.Err(err).Msg("failed to remove thinking reaction")
				}
			}()
		}

		var reaction ReactionStatus
		defer func() {
			if reaction == ReactionStatusZeroValue {
				return
			}

			defer func() {
				if r := recover(); r != nil {
					var err error
					if v, ok := r.(error); ok {
						err = v
					}

					logger.Error().Err(err).Msg("panicked trying to react")
				}
			}()

			err := s.MessageReactionAdd(m.ChannelID, m.ID, reaction.String())
			if err != nil {
				logger.Err(err).Msg("failed to react")
			}
		}()

		for i := range srv.EventHandlers.MessageCreate {

			h := &srv.EventHandlers.MessageCreate[i]

			handler := h.Matcher(p, trimMsg)
			if handler == nil {
				continue
			}

			err := handler(ctx, s, m, p, srv.Brain)
			if err != nil {
				reaction = ReactionStatusErr

				if v, ok := err.(Reactor); ok {
					reaction = v.Reaction()
				}

				logger.Err(err).
					Str("handler_name", h.Name).
					Str("message_content", m.Message.Content).
					Msg("error in handler")

				_, err := s.ChannelMessageSend(m.ChannelID, "error: "+err.Error())
				if err != nil {
					logger.Err(err).
						Msg("failed to send error reply")
				}
				return
			}

			reaction = ReactionStatusOK

			// logger.Info().
			// 	Str("handler_name", h.Name).
			// 	Str("author_id", m.Author.ID).
			// 	Str("author_username", m.Author.Username).
			// 	Str("message_content", m.Message.Content).
			// 	Interface("message_id", m.Message.ID).
			// 	Interface("message_timestamp", m.Message.Timestamp).
			// 	Msg("handled message")

			return
		}

		reaction = ReactionStatusWarning

		// logger.Debug().
		// 	Str("author_id", m.Author.ID).
		// 	Str("author_username", m.Author.Username).
		// 	Str("message_content", m.Message.Content).
		// 	Interface("message_id", m.Message.ID).
		// 	Interface("message_timestamp", m.Message.Timestamp).
		// 	Msg("unhandled message")

		_, err = s.ChannelMessageSend(m.ChannelID, "command not recognized")
		if err != nil {
			logger.Err(err).
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
