package testconfig

import (
	"github.com/josephcopenhaver/melody-bot/internal/service/config"
)

func New() (*config.Config, error) {

	conf := &config.Config{}
	return conf, nil
}
