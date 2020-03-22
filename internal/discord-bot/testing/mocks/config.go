package mocks

import (
	"testing"

	"github.com/josephcopenhaver/discord-bot/internal/discord-bot/config"
	. "github.com/smartystreets/goconvey/convey"
)

func withConfig(confPtr **config.Config, f func()) func() {
	return func() {
		prevConf := *confPtr
		defer func() {
			*confPtr = prevConf
		}()

		*confPtr = &config.Config{}

		f()
	}
}

func WithTConfig(t *testing.T, confPtr **config.Config, f func()) {
	Convey("with T test config", t, withConfig(confPtr, f))
}

func WithConfig(confPtr **config.Config, f func()) {
	Convey("with test config", withConfig(confPtr, f))
}
