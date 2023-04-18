package handlers

import (
	"context"
	"os"
	"regexp"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func ClearCache() HandleMessageCreate {

	// TODO: something about stop calls does not clear the download state fully
	// playlist tracks get reset because the tracks error too much

	return newHandleMessageCreateWithBrain(
		"clear cache",
		"clear cache",
		"stops all players and clears files in the audio cache",
		newRegexMatcherWithBrain(
			false,
			regexp.MustCompile(`^\s*clear(?:-|\s+)cache\s*$`),
			clearCache,
		),
	)
}

func clearCache(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, _ *service.Player, _ map[string]string, b *service.Brain) error {

	err := b.StopAllPlayers(m)
	if err != nil {
		return err
	}

	time.Sleep(time.Second) // TODO: refactor to remove this, remove all should be done in a protected context

	return os.RemoveAll(MediaCacheDir)
}
