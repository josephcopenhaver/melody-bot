package service

import (
	"context"
	"sync"

	"github.com/bwmarrin/discordgo"
)

type Brain struct {
	mutex    sync.Mutex
	guildMap sync.Map
}

func NewBrain() *Brain {
	return &Brain{
		guildMap: sync.Map{},
	}
}

func (b *Brain) Player(ctx context.Context, wg *sync.WaitGroup, s *discordgo.Session, guildId string) *Player {

	var result *Player

	resp, ok := b.guildMap.Load(guildId)
	if ok {
		return resp.(*Player)
	}

	// locking to prevent goroutine leaks
	// and to prevent data-races to create/initialize Players
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// reread in case it was just created
	// by another thread
	resp, ok = b.guildMap.Load(guildId)
	if ok {
		return resp.(*Player)
	}

	result = NewPlayer(ctx, wg, s, guildId)

	b.guildMap.Store(guildId, result)

	return result
}

func (b *Brain) PlayerExists(s *discordgo.Session, guildId string) bool {

	_, ok := b.guildMap.Load(guildId)
	return ok
}
