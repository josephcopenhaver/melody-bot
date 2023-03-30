package handlers

import (
	"context"
	"os"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func ClearCache() HandleMessageCreate {

	return newHandleMessageCreateWithBrain(
		"clearcache",
		"clearcache",
		"stops all players and clears files in the audio cache",
		newWordMatcherWithBrain(
			false,
			[]string{"clearcache"},
			func(_ context.Context, s *discordgo.Session, m *discordgo.MessageCreate, _ *service.Player, b *service.Brain) error {

				err := b.StopAllPlayers(m)
				if err != nil {
					return err
				}

				time.Sleep(time.Second) // TODO: refactor to remove this, remove all should be done in a protected context

				return os.RemoveAll(MediaCacheDir)
			},
		),
	)
}

func clearCache(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, _ *service.Player /*args*/, _ map[string]string, b *service.Brain) error {
	return nil
}
