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

// StopAllPlayers should be refactored into a WithAllPlayersStopped call that takes operational objective options
func (b *Brain) StopAllPlayers(m *discordgo.MessageCreate) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.guildMap.Range(func(key, value any) bool {
		guildId, ok := key.(string)
		if guildId == "" || !ok {
			return true
		}

		p, ok := value.(*Player)
		if p == nil || !ok {
			return true
		}

		p.Stop(m) // TODO: refactor to support an administratively stopped state

		return true
	})

	// TODO: refactor so that a call can be done while all players are in an administratively stopped state
	// TODO: put players back into idle state, but keep track index

	return nil
}

func (b *Brain) PlayerExists(s *discordgo.Session, guildId string) bool {

	_, ok := b.guildMap.Load(guildId)
	return ok
}
