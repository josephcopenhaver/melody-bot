package server

import (
	"testing"

	"github.com/josephcopenhaver/melody-bot/internal/service/testing/testconfig"
	. "github.com/smartystreets/goconvey/convey"
)

func TestNormalHandlerRegistration(t *testing.T) {
	Convey("no error should occur during regular setup", func() {
		conf, err := testconfig.New()
		So(err, ShouldBeNil)

		s := New()
		err = s.SetConfig(conf)
		So(err, ShouldEqual, nil)

		err = s.Handlers()
		So(err, ShouldEqual, nil)
	})
}
