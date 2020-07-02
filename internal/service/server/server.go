package server

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
	"github.com/rs/zerolog/log"
)

type EventHandlers struct {
	MessageCreate []HandleMessageCreate
}

type Server struct {
	DiscordSession *discordgo.Session
	EventHandlers  EventHandlers
	Brain          *service.Brain
}

func New() *Server {
	return &Server{
		EventHandlers: EventHandlers{
			MessageCreate: []HandleMessageCreate{},
		},
		Brain: service.NewBrain(),
	}
}

func (s *Server) ListenAndServe() error {
	// open a connection to discord
	if err := s.DiscordSession.Open(); err != nil {
		return err
	}

	log.Info().
		Msg("listening")

	// wait for a process signal
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	return s.DiscordSession.Close()
}
