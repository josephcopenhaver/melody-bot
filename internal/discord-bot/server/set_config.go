package server

import (
	"github.com/bwmarrin/discordgo"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/josephcopenhaver/discord-bot/internal/discord-bot/config"
)

func (s *Server) SetConfig(conf *config.Config) error {
	var err error

	s.DiscordSession, err = discordgo.New("Bot " + conf.DiscordBotToken)
	if err != nil {
		return err
	}

	return s.ValidateConfig()
}

func (s *Server) ValidateConfig() error {
	return validation.ValidateStruct(s,
		// DiscordSession must not be nil
		validation.Field(&s.DiscordSession, validation.Required),
	)
}
