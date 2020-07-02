package service

import "sync"

type Brain struct {
	mutex    *sync.Mutex
	guildMap *sync.Map
}

func NewBrain() *Brain {
	return &Brain{
		mutex:    &sync.Mutex{},
		guildMap: &sync.Map{},
	}
}

func (b *Brain) Player(guildId string) *Player {

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

	result = NewPlayer(guildId)

	b.guildMap.Store(guildId, result)

	return result
}
