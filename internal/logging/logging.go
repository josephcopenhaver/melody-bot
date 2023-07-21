package logging

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

//nolint:gochecknoinits
func init() {
	var out io.Writer = os.Stderr

	// set the default log level
	zerolog.SetGlobalLevel(zerolog.TraceLevel)

	// allow printing stack traces from pkg/errors
	// code-smell: globals are being set here
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack //nolint:reassign

	// use default golang time marshaller format
	zerolog.TimeFieldFormat = time.RFC3339

	// // report only line number that generates the log entry
	// zerolog.CallerFieldName = "line"
	// zerolog.CallerMarshalFunc = func(file string, line int) string {
	// 	return strconv.Itoa(line)
	// }

	// default to using the console writer unless user overrides
	if strings.ToLower(os.Getenv("LOG_FORMAT")) != "json" {
		out = zerolog.ConsoleWriter{
			Out: out,
		}
	}

	// place line indicators - Caller
	// add stacktrace on .Err() - Stack
	// include timestamps in output - Timestamp
	log.Logger = zerolog.New(out).
		With().
		Caller().
		Stack().
		Timestamp().
		Logger()
}

// SetGlobalLevel should only be called once, and before goroutines are spawned
func SetGlobalLevel(logLevelStr string) error {
	logLevel, err := zerolog.ParseLevel(strings.ToLower(logLevelStr))
	if err != nil {
		return err
	}

	zerolog.SetGlobalLevel(logLevel)

	return nil
}
