package config

import (
	validation "github.com/go-ozzo/ozzo-validation"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	DiscordBotToken string `split_words:"true" required:"true"`
}

func (c *Config) Validate() error {
	return validation.ValidateStruct(c,
		// DiscordBotToken must not be empty
		validation.Field(&c.DiscordBotToken, validation.Required),
	)
}

func New() (*Config, error) {
	conf := &Config{}

	if err := envconfig.Process("", conf); err != nil {
		return nil, err
	}

	return conf, conf.Validate()
}
