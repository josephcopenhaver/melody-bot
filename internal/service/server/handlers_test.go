package server_test

import (
	"context"
	"testing"

	"github.com/josephcopenhaver/melody-bot/internal/service/server"
	"github.com/josephcopenhaver/melody-bot/internal/service/testing/testconfig"
	. "github.com/smartystreets/goconvey/convey"
)

func TestNormalHandlerRegistration(t *testing.T) {
	Convey("no error should occur during regular setup", t, func() {
		conf, err := testconfig.New()
		So(err, ShouldBeNil)

		s := server.New()
		err = s.SetConfig(conf)
		So(err, ShouldEqual, nil)

		err = s.Handlers(context.Background())
		So(err, ShouldEqual, nil)
	})
}
