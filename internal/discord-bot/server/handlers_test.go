package server

import (
	"testing"

	"github.com/josephcopenhaver/discord-bot/internal/discord-bot/config"
	"github.com/josephcopenhaver/discord-bot/internal/discord-bot/testing/mocks"
	. "github.com/smartystreets/goconvey/convey"
)

func TestNormalHandlerRegistration(t *testing.T) {
	var conf *config.Config

	mocks.WithTConfig(t, &conf, func() {
		Convey("no error should occur during regular setup", func() {
			var err error

			s := New()
			err = s.SetConfig(conf)
			So(err, ShouldEqual, nil)

			err = s.Handlers()
			So(err, ShouldEqual, nil)
		})
	})
}
