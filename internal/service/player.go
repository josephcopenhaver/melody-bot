package service

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/bwmarrin/discordgo"
)

// Signal: a command that can be sent to the player
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
	url           string
	audioFile     string
	authorId      string
	authorMention string
}

type playRequest struct {
	*track
}

type PlayerMemory struct {
	voiceConnection *discordgo.VoiceConnection
	notLooping      bool
	currentTrackIdx int
	textChannel     string
	tracks          []track
	playRequests    chan *playRequest
}

func (m *PlayerMemory) reset() {

	if m.voiceConnection != nil {
		err := m.voiceConnection.Disconnect()
		if err != nil {
			log.Err(err).Msg("reset: failed to disconnect")
		}
	}

	*m = PlayerMemory{
		playRequests:    m.playRequests,
		currentTrackIdx: -1,
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

type TracedSignal struct {
	src interface{}
	sig Signal
}

type Player struct {
	mutex          sync.RWMutex
	memory         atomic.Value
	discordSession *discordgo.Session
	signalChan     chan TracedSignal
}

func NewPlayer(s *discordgo.Session, guildId string) *Player {

	p := &Player{
		discordSession: s,
		signalChan:     make(chan TracedSignal, 1),
	}

	playRequests := make(chan *playRequest, 1)

	p.memory.Store(PlayerMemory{
		playRequests:    playRequests,
		currentTrackIdx: -1,
	})
	go playerWorker(p, guildId, p.signalChan)

	return p
}

// // setNextTrackIndex returns true if index is in playlist range
// func (p *Player) setNextTrackIndex(idx int) bool {
// 	var result bool

// 	p.withMemory(func(m *PlayerMemory) {

// 		if idx >= 0 && idx < len(m.tracks) {
// 			result = true
// 			m.currentTrackIdx = idx - 1
// 		}
// 	})

// 	return result
// }

func (p *Player) nextTrack() *track {
	var result *track

	p.withMemory(func(m *PlayerMemory) {

		m.currentTrackIdx++
		if m.currentTrackIdx >= len(m.tracks) {
			if m.notLooping {
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

func (p *Player) Reset(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalReset}
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

func (p *Player) Pause(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalPause}
}

func (p *Player) Stop(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalStop}
}

func (p *Player) Resume(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalResume}
}

func (p *Player) Next(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalNext}
}

func (p *Player) Previous(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalPrevious}
}

func (p *Player) CycleRepeatMode(srcEvt interface{}) string {
	var result string

	p.withMemory(func(m *PlayerMemory) {
		m.notLooping = !m.notLooping

		if m.notLooping {
			result = "not repeating playlist"
		} else {
			result = "repeating playlist"
		}
	})

	return result
}

func (p *Player) Play(srcEvt interface{}, url string, authorId, authorMention string, file string) bool {
	var result bool

	p.withMemory(func(m *PlayerMemory) {

		m.playRequests <- &playRequest{
			track: &track{
				url:           url,
				audioFile:     file,
				authorId:      authorId,
				authorMention: authorMention,
			},
		}

		p.signalChan <- TracedSignal{srcEvt, SignalPlay}

	})

	return result
}

func (p *Player) SetVoiceConnection(srcEvt interface{}, c *discordgo.VoiceConnection) {

	p.withMemory(func(m *PlayerMemory) {

		m.voiceConnection = c
	})

	p.signalChan <- TracedSignal{srcEvt, SignalNewVoiceConnection}
}

func (p *Player) ClearPlaylist(srcEvt interface{}) {

	p.withMemory(func(m *PlayerMemory) {

		m.tracks = nil
	})
}

func (p *Player) RestartTrack(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalRestartTrack}
}

func (p *Player) SetTextChannel(s string) {
	p.withMemory(func(m *PlayerMemory) {
		m.textChannel = s
	})

	p.broadcastTextMessage(nil, "text channel is now this one")
}

func (p *Player) setDefaultTextChannel(v interface{}) {

	switch e := v.(type) {
	case *discordgo.MessageCreate:
		if e == nil {
			return
		}

		p.SetTextChannel(e.Message.ChannelID)
	default:
		if v == nil {
			return
		}

		log.Error().
			Msg("failed to get text channel from first event sent to player")
	}
}

func (p *Player) broadcastTextMessage(debug func() *zerolog.Event, s string) {
	var c string

	p.withMemory(func(m *PlayerMemory) {
		c = m.textChannel
	})

	if c == "" {
		return
	}

	if debug != nil {
		l := debug()
		if l != nil {
			l.
				Str("notification_message", s).
				Msg("broadcasting notification")
		}
	}

	_, err := p.discordSession.ChannelMessageSend(c, s)
	if err != nil {
		log.Err(err).
			Str("notification_message", s).
			Msg("failed to send message")
	}
}

func (p *Player) sendChannel(debug func() *zerolog.Event) chan<- []byte {
	var c *discordgo.VoiceConnection
	var result chan<- []byte

	p.withMemory(func(m *PlayerMemory) {
		c = m.voiceConnection
	})

	if c == nil {
		p.broadcastTextMessage(debug, "no active voice channel")
		return result
	}

	c.Lock()
	defer c.Unlock()

	if !c.Ready {
		p.broadcastTextMessage(debug, "voice channel not ready")
		return result
	}

	result = c.OpusSend

	if result == nil {
		p.broadcastTextMessage(debug, "voice channel has no sender")
	}

	return result
}

func playerWorker(p *Player, guildId string, sigChan <-chan TracedSignal) {

	state := StateDefault
	niceness := 19

	debug := func() *zerolog.Event {
		return log.Debug().
			Interface("state", state).
			Str("guild_id", guildId)
	}

	setNiceness := func(n int) error {

		if niceness == n {

			return nil
		}

		err := SetNiceness(n)
		if err != nil {
			return err
		}

		niceness = n

		return nil
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
			err := playerMainLoop(p, &state, debug, setNiceness, sigChan)
			if err != nil {
				log.Err(err).Msg("player: error occured during playback")
			}
		}()
	}
}

func playerMainLoop(p *Player, statePtr *State, debug func() *zerolog.Event, setNiceness func(int) error, sigChan <-chan TracedSignal) error {
	var err error
	var sendChan chan<- []byte

	debug().Msg("player: main loop start: signal check")

	switch *statePtr {
	case StateDefault:
		// signal trap 1/4:
		// type: blocking
		// signals recognized when in initial state
		s := <-sigChan

		p.setDefaultTextChannel(s.src)

		debug().Msg("player: got signal before playing track")

		switch s.sig {
		case SignalNewVoiceConnection:
			sendChan = nil
		case SignalPlay:
			p.withMemory(func(m *PlayerMemory) {
				m.play(*statePtr)
			})
			*statePtr = StatePlaying
		}
	case StateIdle:
		err = setNiceness(19)
		if err != nil {
			return err
		}

		// signal trap 2/4:
		// type: blocking
		// signals recognized when in idle state ( stopped or partially errored )
		s := <-sigChan

		debug().Msg("player: got signal before playing track")

		switch s.sig {
		case SignalNewVoiceConnection:
			sendChan = nil
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
	} else {

		msg := "now playing: " + track.url
		if track.authorMention != "" {
			msg += " ( added by " + track.authorMention + " )"
		}

		p.broadcastTextMessage(debug, msg)
	}

	err = setNiceness(0)
	if err != nil {
		return err
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

	for {

		noSignal := false

		// debug().Msg("player: broadcast loop start: signal check")

		select {
		// signal trap 3/4:
		// type: non-blocking
		// signals recognized when in playing state
		case s := <-sigChan:
			switch s.sig {
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
				return nil
			case SignalStop:
				p.restartTrack()
				*statePtr = StateIdle
				return nil
			case SignalReset:
				p.reset()
				*statePtr = StateIdle
				return nil
			case SignalNext:
				return nil
			case SignalRestartTrack:
				p.restartTrack()
				return nil
			case SignalPause:
				*statePtr = StatePaused

				err = setNiceness(19)
				if err != nil {
					return err
				}

			PausedLoop:
				for {
					// signal trap 4/4:
					// type: blocking
					// signals recognized when in paused state
					s := <-sigChan
					switch s.sig {
					case SignalNewVoiceConnection:
						sendChan = nil
					case SignalPlay:
						p.withMemory(func(m *PlayerMemory) {
							m.play(*statePtr)
						})
					case SignalPrevious:
						p.previousTrack()
						*statePtr = StateIdle
						return nil
					case SignalNext:
						*statePtr = StateIdle
						return nil
					case SignalStop:
						p.restartTrack()
						*statePtr = StateIdle
						return nil
					case SignalRestartTrack:
						p.restartTrack()
						*statePtr = StateIdle
						return nil
					case SignalReset:
						p.reset()
						*statePtr = StateIdle
						return nil
					case SignalResume:
						*statePtr = StatePlaying
						break PausedLoop
					}
				}

				err = setNiceness(0)
				if err != nil {
					return err
				}

				// rediscover the channel we need to send on
				// if it was altered while paused
				if sendChan == nil {
					sendChan = p.sendChannel(debug)
					if sendChan == nil {
						return nil
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
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("error reading file: %s: %v", track.audioFile, err)
		}

		if numBytes == 0 {
			return nil
		}

		sendChan <- outPackets[outPacketIdx][:numBytes]

		outPacketIdx++
		if outPacketIdx >= NumPacketBuffers {
			outPacketIdx = 0
		}
	}
}
