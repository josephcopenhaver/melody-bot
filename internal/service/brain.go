package service

import (
	"context"
	"errors"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// SyncMap is a generic wrapper around sync's Map type
type SyncMap[K comparable, V any] struct {
	m sync.Map
}

func (m *SyncMap[K, V]) Delete(key K) {
	m.m.Delete(key)
}

func (m *SyncMap[K, V]) Load(key K) (V, bool) {
	var result V

	v, ok := m.m.Load(key)
	if ok {
		var assertOK bool
		result, assertOK = v.(V)
		if !assertOK {
			panic(errors.New("unreachable"))
		}
	}

	return result, ok
}

func (m *SyncMap[K, V]) LoadAndDelete(key K) (V, bool) {
	var result V

	v, loaded := m.m.LoadAndDelete(key)
	if loaded {
		var assertOK bool
		result, assertOK = v.(V)
		if !assertOK {
			panic(errors.New("unreachable"))
		}
	}

	return result, loaded
}

func (m *SyncMap[K, V]) LoadOrStore(key K, value V) (V, bool) {
	var result V

	v, loaded := m.m.LoadOrStore(key, value)
	if loaded {
		var assertOK bool
		result, assertOK = v.(V)
		if !assertOK {
			panic(errors.New("unreachable"))
		}
	} else {
		result = value
	}

	return result, loaded
}

func (m *SyncMap[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(key, value any) bool {

		k, ok := key.(K)
		if !ok {
			panic(errors.New("unreachable"))
		}

		v, ok := value.(V)
		if !ok {
			panic(errors.New("unreachable"))
		}

		return f(k, v)
	})
}

func (m *SyncMap[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

type Brain struct {
	mutex            sync.Mutex
	playersByGuildID SyncMap[string, *Player]
}

func NewBrain() *Brain {
	return &Brain{
		playersByGuildID: SyncMap[string, *Player]{},
	}
}

func (b *Brain) Player(ctx context.Context, wg *sync.WaitGroup, s *discordgo.Session, guildId string) *Player {

	result, ok := b.playersByGuildID.Load(guildId)
	if ok {
		return result
	}

	// locking to prevent goroutine leaks
	// and to prevent data-races to create/initialize Players
	b.mutex.Lock()
	defer b.mutex.Unlock()

	// reread in case it was just created
	// by another thread
	result, ok = b.playersByGuildID.Load(guildId)
	if ok {
		return result
	}

	result = NewPlayer(ctx, wg, s, guildId)

	b.playersByGuildID.Store(guildId, result)

	return result
}

// StopAllPlayers should be refactored into a WithAllPlayersStopped call that takes operational objective options
func (b *Brain) StopAllPlayers(m *discordgo.MessageCreate) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.playersByGuildID.Range(func(guildID string, p *Player) bool {
		if guildID == "" {
			return true
		}

		if p == nil {
			return true
		}

		p.Stop(m) // TODO: refactor to support an administratively stopped state

		return true
	})

	// TODO: refactor so that a call can be done while all players are in an administratively stopped state
	// TODO: put players back into idle state, but keep track index

	return nil
}

func (b *Brain) PlayerExists(_ *discordgo.Session, guildId string) bool {

	_, ok := b.playersByGuildID.Load(guildId)
	return ok
}
