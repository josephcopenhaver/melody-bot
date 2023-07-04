package server

import (
	"context"
	"errors"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
	"github.com/josephcopenhaver/melody-bot/internal/service/handlers"
	"github.com/rs/zerolog/log"
)

type EventHandlers struct {
	MessageCreate []handlers.HandleMessageCreate
}

type Server struct {
	ctx            context.Context
	wg             sync.WaitGroup
	DiscordSession *discordgo.Session
	EventHandlers  EventHandlers
	Brain          *service.Brain
}

func New() *Server {
	return &Server{
		EventHandlers: EventHandlers{
			MessageCreate: []handlers.HandleMessageCreate{},
		},
		Brain: service.NewBrain(),
	}
}

func (s *Server) ListenAndServe(ctx context.Context) (err_result error) {

	if err := ctx.Err(); err != nil {
		return err
	}

	s.ctx = ctx

	sd := handlers.SerialDownloader()
	sd.Start(ctx)
	defer func() {
		log.Warn().
			Msg("waiting for cache downloader to terminate")

		sd.Wait()
	}()

	// open a connection to discord
	if err := s.DiscordSession.Open(); err != nil {
		return err
	}
	defer func() {
		log.Warn().
			Msg("waiting for discord session to close")

		err_result = errors.Join(err_result, s.DiscordSession.Close())
	}()

	defer func() {
		log.Warn().
			Msg("waiting for all players to terminate")

		s.wg.Wait()
	}()

	log.Info().
		Msg("listening")

	<-ctx.Done()

	return nil // fake return, err_result can be set elsewhere
}
