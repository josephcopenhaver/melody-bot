package logging

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
)

// TODO: if an error implements fmt.Formatter then treat it's output as the stack trace
//
// examples of this can be found in https://github.com/pkg/errors and more

func newLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     level,
	}))
}

var logger *slog.Logger

//nolint:gochecknoinits
func init() {
	logger = newLogger(slog.LevelInfo)
	slog.SetDefault(logger)
}

// SetDefaultLevel should only be called once, and before goroutines are spawned
func SetDefaultLevel(logLevelStr string) error {

	var newLevel slog.Level
	if err := newLevel.UnmarshalText([]byte(logLevelStr)); err != nil {
		return err
	}

	logger = newLogger(newLevel)

	slog.SetDefault(logger)

	return nil
}

type loggerCtxKey struct{}

func AddToContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey{}, logger)
}

type logResolver struct {
	f func() *slog.Logger
	sync.RWMutex
	logger *slog.Logger
}

func (lr *logResolver) Get() *slog.Logger {
	lr.RLock()
	cleanup := lr.RUnlock
	defer func() {
		if f := cleanup; f != nil {
			cleanup = nil
			f()
		}
	}()

	if logger := lr.logger; logger != nil {
		return logger
	}

	if f := cleanup; f != nil {
		cleanup = nil
		f()
	}
	lr.Lock()
	cleanup = lr.Unlock

	if logger := lr.logger; logger != nil {
		return logger
	}

	lr.logger = lr.f()
	lr.f = nil

	return lr.logger
}

var logResolverPool = sync.Pool{
	New: func() any {
		return &logResolver{}
	},
}

func AddResolverToContext(ctx context.Context, f func() *slog.Logger) (context.Context, context.CancelFunc) {
	if f == nil {
		panic(errors.New("log resolver function must not be nil"))
	}

	v, ok := logResolverPool.Get().(*logResolver)
	if !ok || v == nil {
		panic(errors.New("unreachable"))
	}

	v.f = f
	v.logger = nil

	return context.WithValue(ctx, loggerCtxKey{}, v), sync.OnceFunc(func() {
		v.f = nil
		v.logger = nil
		logResolverPool.Put(v)
	})
}

func Context(ctx context.Context) *slog.Logger {
	ctxVal := ctx.Value(loggerCtxKey{})
	if ctxVal == nil {
		return slog.Default()
	}

	switch v := ctxVal.(type) {
	case *slog.Logger:
		return v
	case *logResolver:
		return v.Get()
	default:
	}

	return slog.Default()
}
