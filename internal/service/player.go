package service

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/bwmarrin/discordgo"
)

// Signal: a command that can be send to the player
type Signal int8

const (
	SignalUnusedLower Signal = iota - 1
	//
	SignalNewVoiceConnection
	SignalPlay
	SignalResume
	SignalPause
	SignalStop
	SignalReset
	SignalNext
	SignalPrevious
	SignalRestartTrack
	//
	SignalUnusedUpper
)

func (s Signal) String() string {
	return []string{
		"new-voice-connection",
		"play",
		"resume",
		"pause",
		"stop",
		"reset",
		"next",
		"previous",
		"restart-track",
	}[int(s)]
}

type State int8

const (
	StateUnusedLower State = iota - 1
	//
	StateDefault
	StateIdle
	StatePlaying
	StatePaused
	//
	StateUnusedUpper
)

func (s State) String() string {
	return []string{
		"default",
		"idle",
		"playing",
		"paused",
	}[int(s)]
}

type track struct {
	url       string
	audioFile string
}

type playRequest struct {
	*track
}

type PlayerMemory struct {
	voiceConnection *discordgo.VoiceConnection
	looping         bool
	currentTrackIdx int
	// logChannelId *string // TODO: make this a thing
	tracks       []track
	playRequests chan *playRequest
}

func (m *PlayerMemory) reset() {

	if m.voiceConnection != nil {
		err := m.voiceConnection.Disconnect()
		if err != nil {
			log.Err(err).Msg("reset: failed to disconnect")
		}
	}

	*m = PlayerMemory{
		playRequests: m.playRequests,
	}
}

func (m *PlayerMemory) indexOfTrack(file string) int {

	for i, t := range m.tracks {
		if t.audioFile == file {
			return i
		}
	}

	return -1
}

func (m *PlayerMemory) play(s State) {

	r := <-m.playRequests

	if r.track != nil {
		t := *r.track

		switch s {
		case StateDefault:
			m.tracks = []track{t}
			m.currentTrackIdx = -1
		case StateIdle:
			i := m.indexOfTrack(t.audioFile)
			if i < 0 {
				m.tracks = append(m.tracks, t)
			}
			m.currentTrackIdx = len(m.tracks) - 2
		case StatePaused:
			i := m.indexOfTrack(t.audioFile)
			if i < 0 {
				m.tracks = append(m.tracks, t)
			}
		case StatePlaying:
			i := m.indexOfTrack(t.audioFile)
			if i < 0 {
				m.tracks = append(m.tracks, t)
			}
		}
	}
}

type Player struct {
	mutex      sync.RWMutex
	memory     atomic.Value
	signalChan chan Signal
}

func NewPlayer(guildId string) *Player {

	p := &Player{
		signalChan: make(chan Signal, 1),
	}

	playRequests := make(chan *playRequest, 1)

	p.memory.Store(PlayerMemory{
		playRequests: playRequests,
	})
	go playerWorker(p, guildId, p.signalChan)

	return p
}

// setNextTrackIndex returns true if index is in playlist range
func (p *Player) setNextTrackIndex(idx int) bool {
	var result bool

	p.withMemory(func(m *PlayerMemory) {

		if idx >= 0 && idx < len(m.tracks) {
			result = true
			m.currentTrackIdx = idx - 1
		}
	})

	return result
}

func (p *Player) nextTrack() *track {
	var result *track

	p.withMemory(func(m *PlayerMemory) {

		m.currentTrackIdx++
		if m.currentTrackIdx >= len(m.tracks) {
			if !m.looping {
				m.currentTrackIdx = -1
				return
			}
			m.currentTrackIdx = 0
		}

		if m.currentTrackIdx < len(m.tracks) {
			track := m.tracks[m.currentTrackIdx]
			result = &track
			return
		}

		m.currentTrackIdx = -1
	})

	return result
}

func (p *Player) withMemoryErr(f func(m *PlayerMemory) error) error {

	// log.Debug().Msg("withMemory: waiting for lock")

	p.mutex.Lock()
	defer p.mutex.Unlock()

	// defer log.Debug().Msg("withMemory: releasing lock")
	// log.Debug().Msg("withMemory: got lock")

	resp := p.memory.Load()

	m, ok := resp.(PlayerMemory)
	if !ok {
		panic("my brain has been corrupted!")
	}

	err := f(&m)
	if err != nil {
		return err
	}

	p.memory.Store(m)

	return nil
}

func (p *Player) withMemory(f func(m *PlayerMemory)) {

	// will never actually have an error
	_ = p.withMemoryErr(func(m *PlayerMemory) error {

		f(m)

		return nil
	})
}

func (p *Player) Reset() {

	p.signalChan <- SignalReset
}

func (p *Player) reset() {
	p.withMemory(func(m *PlayerMemory) {
		m.reset()
	})
}

func (p *Player) restartTrack() {
	p.withMemory(func(m *PlayerMemory) {
		m.currentTrackIdx = m.currentTrackIdx - 1
	})
}

func (p *Player) previousTrack() {
	p.withMemory(func(m *PlayerMemory) {
		if len(m.tracks) < 2 {
			m.currentTrackIdx = -1
		}
		m.currentTrackIdx -= 2
		if m.currentTrackIdx < -1 {
			m.currentTrackIdx = len(m.tracks) - 2
		}
	})
}

func (p *Player) Pause() {

	p.signalChan <- SignalPause
}

func (p *Player) Stop() {

	p.signalChan <- SignalStop
}

func (p *Player) Resume() {

	p.signalChan <- SignalResume
}

func (p *Player) Next() {

	p.signalChan <- SignalNext
}

func (p *Player) Previous() {

	p.signalChan <- SignalPrevious
}

func (p *Player) CycleRepeatMode() string {
	var result string

	p.withMemory(func(m *PlayerMemory) {
		m.looping = !m.looping

		if m.looping {
			result = "repeating playlist"
		} else {
			result = "not repeating playlist"
		}
	})

	return result
}

func (p *Player) Play(url string, file string) bool {
	var result bool

	p.withMemory(func(m *PlayerMemory) {

		m.playRequests <- &playRequest{
			track: &track{
				url:       url,
				audioFile: file,
			},
		}

		p.signalChan <- SignalPlay

	})

	return result
}

func (p *Player) SetVoiceConnection(c *discordgo.VoiceConnection) {

	p.withMemory(func(m *PlayerMemory) {

		m.voiceConnection = c
	})

	p.signalChan <- SignalNewVoiceConnection
}

func (p *Player) ClearPlaylist() {

	p.withMemory(func(m *PlayerMemory) {

		m.tracks = nil
	})
}

func (p *Player) RestartTrack() {

	p.signalChan <- SignalRestartTrack
}

func (p *Player) sendChannel(debug func() *zerolog.Event) chan<- []byte {
	var c *discordgo.VoiceConnection
	var result chan<- []byte

	p.withMemory(func(m *PlayerMemory) {
		c = m.voiceConnection
	})

	if c == nil {
		// TODO: send message to text channel
		debug().Msg("no active voice channel")
		return result
	}

	c.Lock()
	defer c.Unlock()

	if !c.Ready {
		// TODO: send message to text channel
		debug().Msg("voice channel not ready")
		return result
	}

	result = c.OpusSend

	if result == nil {
		// TODO: send message to text channel
		debug().Msg("voice channel has invalid sending channel")
	}

	return result
}

func playerWorker(p *Player, guildId string, sigChan <-chan Signal) {

	state := StateDefault

	debug := func() *zerolog.Event {
		return log.Debug().
			Interface("state", state).
			Str("guild_id", guildId)
	}

	defer func() {
		debug().Msg("player: permanently broken")
	}()

	debug().Msg("player: starting")

	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().
						Interface("error", r).
						Interface("state", state).
						Str("guild_id", guildId).
						Msg("player: recovered from panic")
				}
			}()
			err := playerMainLoop(p, &state, debug, sigChan)
			if err != nil {
				log.Err(err).Msg("player: error occured during playback")
			}
		}()
	}
}

func playerMainLoop(p *Player, statePtr *State, debug func() *zerolog.Event, sigChan <-chan Signal) error {
	var sendChan chan<- []byte

	debug().Msg("player: main loop start: signal check")

	switch *statePtr {
	case StateDefault:
		s := <-sigChan

		debug().Msg("player: got signal before playing track")

		switch s {
		case SignalNewVoiceConnection:
			sendChan = p.sendChannel(debug)
		case SignalPlay:
			p.withMemory(func(m *PlayerMemory) {
				m.play(*statePtr)
			})
			*statePtr = StatePlaying
		case SignalResume:
			if p.setNextTrackIndex(0) {
				*statePtr = StatePlaying
			}
		}
	case StateIdle:
		s := <-sigChan

		debug().Msg("player: got signal before playing track")

		switch s {
		case SignalNewVoiceConnection:
			sendChan = p.sendChannel(debug)
		case SignalPlay:
			p.withMemory(func(m *PlayerMemory) {
				m.play(*statePtr)
			})
			*statePtr = StatePlaying
		case SignalResume:
			*statePtr = StatePlaying
		case SignalNext:
			// do nothing, let loop normally advance
		case SignalPrevious:
			p.withMemory(func(m *PlayerMemory) {
				m.currentTrackIdx -= 1
				if m.currentTrackIdx < -1 {
					m.currentTrackIdx = len(m.tracks) - 1
				}
			})
		}
	}

	debug().Msg("player: play check")

	if *statePtr != StatePlaying {

		debug().Msg("player: not playing")

		*statePtr = StateIdle

		return nil
	}

	if sendChan == nil {
		sendChan = p.sendChannel(debug)

		if sendChan == nil {

			debug().Msg("player: trying to play, but no broadcast channel is ready")

			*statePtr = StateIdle

			return nil
		}
	}

	track := p.nextTrack()
	if track == nil {
		*statePtr = StateIdle
		return nil
	}

	f, err := os.Open(track.audioFile)
	if err != nil {
		return fmt.Errorf("failed to open audio file: %s: %v", track.audioFile, err)
	}
	defer f.Close()

	// read packets from file and buffer them to send to broadcast channel

	opusReader := NewOpusReader(f)
	outPackets := [NumPacketBuffers][SampleMaxBytes]byte{}
	outPacketIdx := 0

BroadcastTrackLoop:
	for {

		noSignal := false

		// debug().Msg("player: broadcast loop start: signal check")

		select {
		case s := <-sigChan:
			switch s {
			case SignalNewVoiceConnection:
				sendChan = p.sendChannel(debug)
				if sendChan == nil {
					return nil
				}
			case SignalPlay:
				p.withMemory(func(m *PlayerMemory) {
					m.play(*statePtr)
				})
			case SignalPrevious:
				p.previousTrack()
				break BroadcastTrackLoop
			case SignalStop:
				p.restartTrack()
				*statePtr = StateIdle
				break BroadcastTrackLoop
			case SignalReset:
				p.reset()
				*statePtr = StateIdle
				break BroadcastTrackLoop
			case SignalNext:
				break BroadcastTrackLoop
			case SignalRestartTrack:
				p.restartTrack()
				break BroadcastTrackLoop
			case SignalPause:
				*statePtr = StatePaused
			PausedLoop:
				for {
					s := <-sigChan
					switch s {
					case SignalNewVoiceConnection:
						sendChan = p.sendChannel(debug)
						if sendChan == nil {
							return nil
						}
					case SignalPlay:
						p.withMemory(func(m *PlayerMemory) {
							m.play(*statePtr)
						})
					case SignalPrevious:
						p.previousTrack()
						*statePtr = StateIdle
						break BroadcastTrackLoop
					case SignalNext:
						*statePtr = StateIdle
						break BroadcastTrackLoop
					case SignalStop:
						p.restartTrack()
						*statePtr = StateIdle
						break BroadcastTrackLoop
					case SignalRestartTrack:
						p.restartTrack()
						break BroadcastTrackLoop
					case SignalReset:
						*statePtr = StateIdle
						p.reset()
						break BroadcastTrackLoop
					case SignalResume:
						break PausedLoop
					}
				}
			}
		default:
			noSignal = true
		}

		if !noSignal {
			debug().Msg("player: processed signal while playing")
		}

		numBytes, err := opusReader.ReadPacket(outPackets[outPacketIdx][:])
		if err != nil {
			return fmt.Errorf("error reading file: %s: %v", track.audioFile, err)
		}

		if numBytes == 0 {
			break
		}

		sendChan <- outPackets[outPacketIdx][:numBytes]

		outPacketIdx++
		if outPacketIdx >= NumPacketBuffers {
			outPacketIdx = 0
		}
	}

	return nil
}
