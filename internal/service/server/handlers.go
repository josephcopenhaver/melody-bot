package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/logging"
	"github.com/josephcopenhaver/melody-bot/internal/service"
	"github.com/josephcopenhaver/melody-bot/internal/service/handlers"
	"github.com/josephcopenhaver/melody-bot/internal/service/server/reactions"
)

//nolint:gocyclo
func (s *Server) Handlers(ctx context.Context) error {

	// https://discord.com/developers/docs/topics/gateway#event-names

	s.addMuxHandlers(ctx)

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

	s.AddHandler(handlers.Cache())

	s.DiscordSession.AddHandler(func(session *discordgo.Session, evt *discordgo.VoiceStateUpdate) {
		// https://discord.com/developers/docs/topics/gateway#voice-state-update
		// Sent when someone joins/leaves/moves voice channels. Inner payload is a voice state object.

		if ctx.Err() != nil {
			return
		}

		slog.Debug(
			"evt: join/leave/move voice channel",
		)

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

		p := s.Brain.Player(ctx, &s.wg, session, evt.VoiceState.GuildID)
		if p.HasAudience() {
			return
		}

		slog.Debug(
			"no audience, pausing",
		)

		p.Pause(evt)
	})

	s.DiscordSession.AddHandler(func(session *discordgo.Session, evt *discordgo.GuildDelete) {
		// https://discord.com/developers/docs/topics/gateway#guild-delete
		// Sent when a guild becomes unavailable during a guild outage, or when the user leaves or is removed from a guild. The inner payload is an unavailable guild object. If the unavailable field is not set, the user was removed from the guild.

		if ctx.Err() != nil {
			return
		}

		slog.Debug(
			"evt: guild unavailable",
		)

		if evt.ID == "" || evt.Unavailable {
			return
		}

		// if player does not exist then short circuit
		if !s.Brain.PlayerExists(session, evt.ID) {
			return
		}

		p := s.Brain.Player(ctx, &s.wg, session, evt.ID)

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

		if ctx.Err() != nil {
			return
		}

		slog.Debug(
			"evt: channel deleted",
		)

		if evt.Channel == nil || evt.Channel.GuildID == "" {
			return
		}

		// if player does not exist then short circuit
		if !s.Brain.PlayerExists(session, evt.Channel.GuildID) {
			return
		}

		p := s.Brain.Player(ctx, &s.wg, session, evt.Channel.GuildID)

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

//nolint:gocyclo
func (s *Server) addMuxHandlers(ctx context.Context) {
	srv := s
	srv.DiscordSession.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		var err error
		var p *service.Player

		// ignore messages I (the bot) create
		if m.Author.ID == s.State.User.ID {
			return
		}

		if ctx.Err() != nil {
			return
		}

		trimMsg := strings.TrimSpace(m.Message.Content)

		if m.GuildID != "" {

			p = srv.Brain.Player(ctx, &srv.wg, s, m.GuildID)

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

				// slog.Debug(
				// 	"no match",
				// 	"channel_message", trimMsg,
				// 	"prefix", prefix,
				// 	"mention", "user",
				// )

				member, err := s.State.Member(m.GuildID, s.State.User.ID)
				if err != nil {
					slog.Error(
						"failed to get my own member status",
						"error", err,
					)
					return ""
				}

				prefix = member.Mention()
				if strings.HasPrefix(trimMsg, prefix) {
					return prefix
				}

				// slog.Debug(
				// 	"no match",
				// 	"channel_message", trimMsg,
				// 	"prefix", prefix,
				// 	"mention", "member",
				// )

				for _, roleId := range member.Roles {

					r, err := s.State.Role(m.GuildID, roleId)
					if err != nil {
						slog.Error(
							"failed to get role info",
							"error", err,
							"role_id", roleId,
						)
						continue
					}

					if r.Name != s.State.User.Username {
						// slog.Debug(
						// 	"role name does not match",
						// 	"role_name", r.Name,
						// 	"user_name", s.State.User.Username,
						// )
						continue
					}

					prefix = r.Mention()
					if strings.HasPrefix(trimMsg, prefix) {
						return prefix
					}

					// slog.Debug(
					// 	"no match",
					// 	"channel_message", trimMsg,
					// 	"prefix", prefix,
					// 	"mention", "role",
					// )
				}

				return ""
			}()

			if prefix == "" {
				// slog.Debug(
				// 	"message not for me",
				// 	"channel_message", trimMsg,
				// )
				return
			}

			// get message without @bot directive
			{
				withoutMention := trimMsg[len(prefix):]
				newTrimMsg := strings.TrimSpace(withoutMention)
				if newTrimMsg == withoutMention {
					// slog.Debug(
					// 	"not well formed for me",
					// 	"channel_message", trimMsg,
					// )
					return
				}

				trimMsg = newTrimMsg
			}
		}

		var logger *slog.Logger
		{
			log := slog.With(
				"guild_id", m.GuildID,
				"channel_id", m.ChannelID,
				"message_id", m.Message.ID,
				"message_timestamp", m.Timestamp,
				"author_id", m.Author.ID,
				"author_username", m.Author.Username,
			)

			if m.EditedTimestamp != nil {
				log = log.With("last_edited_at", *m.EditedTimestamp)
			}

			logger = log
		}

		ctxWithLogger := logging.AddToContext(ctx, logger)

		err = s.MessageReactionAdd(m.ChannelID, m.ID, reactions.StatusThinking.String())
		if err != nil {
			slog.Error(
				"failed to react with thinking",
				"error", err,
			)
		} else {
			defer func() {
				err := s.MessageReactionRemove(m.ChannelID, m.ID, reactions.StatusThinking.String(), "@me")
				if err != nil {
					slog.Error(
						"failed to remove thinking reaction",
						"error", err,
					)
				}
			}()
		}

		var reaction reactions.Status
		defer func() {
			if reaction == reactions.StatusZeroValue {
				return
			}

			defer func() {
				if r := recover(); r != nil {
					var err error
					if v, ok := r.(error); ok {
						err = v
					}

					slog.Error(
						"panicked trying to react",
						"error", err,
					)
				}
			}()

			err := s.MessageReactionAdd(m.ChannelID, m.ID, reaction.String())
			if err != nil {
				slog.Error(
					"failed to react",
					"error", err,
				)
			}
		}()

		for i := range srv.EventHandlers.MessageCreate {

			h := &srv.EventHandlers.MessageCreate[i]

			handler := h.Matcher(p, trimMsg)
			if handler == nil {
				continue
			}

			err := handler(ctxWithLogger, s, m, p, srv.Brain)
			if err != nil {
				reaction = reactions.StatusErr

				var v reactions.Reactor
				if ok := errors.As(err, &v); ok {
					reaction = v.Reaction()
				}

				slog.Error(
					"error in handler",
					"error", err,
					"handler_name", h.Name,
					"message_content", m.Message.Content,
				)

				_, err := s.ChannelMessageSend(m.ChannelID, "error: "+err.Error())
				if err != nil {
					slog.Error(
						"failed to send error reply",
						"error", err,
					)
				}
				return
			}

			reaction = reactions.StatusOK

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

		reaction = reactions.StatusWarning

		// logger.Debug().
		// 	Str("author_id", m.Author.ID).
		// 	Str("author_username", m.Author.Username).
		// 	Str("message_content", m.Message.Content).
		// 	Interface("message_id", m.Message.ID).
		// 	Interface("message_timestamp", m.Message.Timestamp).
		// 	Msg("unhandled message")

		_, err = s.ChannelMessageSend(m.ChannelID, "command not recognized")
		if err != nil {
			slog.Error(
				"failed to send default reply",
				"error", err,
			)
		}
	})
}

func (s *Server) AddHandler(v interface{}) {

	switch h := v.(type) {

	case handlers.HandleMessageCreate:
		s.EventHandlers.MessageCreate = append(s.EventHandlers.MessageCreate, h)

	default:
		const msg = "code-error: failed to register handler"
		slog.Error(
			msg,
			"handler", v,
		)
		panic(errors.New("code-error: failed to register handler"))
	}
}
