package service

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/gopus"
)

// transcoding constants
const (
	SampleRate       = 48000 // kits per second
	NumChannels      = 1
	SampleSize       = 960 // int16 size of each audio frame
	SampleMaxBytes   = SampleSize * 2 * NumChannels
	NumPacketBuffers = 4 // should always be 2 greater than the OpusSend channel packet size to ensure no buffer lag occurs and no corruption occurs, this also avoids allocations and reduces CPU burn
)

// Signal: a command that can be send to the player
type Signal int8

const (
	SignalUnusedLower Signal = iota - 1
	//
	SignalPlay
	SignalResume
	SignalPause
	SignalStop
	SignalReset
	SignalNext
	SignalPrevious
	//
	SignalUnusedUpper
)

func (s Signal) String() string {
	return []string{
		"play",
		"resume",
		"pause",
		"stop",
		"reset",
		"next",
		"previous",
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
	tracks          []track
	playRequestChan chan *playRequest
}

func (m *PlayerMemory) reset() {

	if m.voiceConnection != nil {
		err := m.voiceConnection.Disconnect()
		if err != nil {
			log.Err(err).Msg("reset: failed to disconnect")
		}
	}

	*m = PlayerMemory{
		playRequestChan: m.playRequestChan,
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

func (m *PlayerMemory) play(s *State) {

	r := <-m.playRequestChan

	if r.track != nil {
		t := *r.track

		switch *s {
		case StateDefault:
			m.tracks = []track{t}
			m.currentTrackIdx = -1
			*s = StatePlaying
		case StateIdle:
			i := m.indexOfTrack(t.audioFile)
			if i < 0 {
				m.tracks = append(m.tracks, t)
			}
			m.currentTrackIdx = len(m.tracks) - 2
			*s = StatePlaying
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

	playRequestChan := make(chan *playRequest, 1)

	p.memory.Store(PlayerMemory{
		playRequestChan: playRequestChan,
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

func (p *Player) voiceConnection() *discordgo.VoiceConnection {
	var result *discordgo.VoiceConnection

	p.withMemory(func(m *PlayerMemory) {

		result = m.voiceConnection
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

// TODO: maybe change playing state when they skip?
func (p *Player) previousTrack(s *State) {
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

		m.playRequestChan <- &playRequest{
			track: &track{
				url:       url,
				audioFile: file,
			},
		}

		p.signalChan <- SignalPlay

	})

	return result
}

// TODO: if playing, should pause and then unpause to get new channel
func (p *Player) SetVoiceConnection(c *discordgo.VoiceConnection) {

	p.withMemory(func(m *PlayerMemory) {

		m.voiceConnection = c
	})
}

func (p *Player) ClearPlaylist() {

	p.withMemory(func(m *PlayerMemory) {

		m.tracks = nil
	})
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
			playerMainLoop(p, &state, debug, sigChan)
		}()
	}
}

func playerMainLoop(p *Player, statePtr *State, debug func() *zerolog.Event, sigChan <-chan Signal) {

	debug().Msg("player: main loop start")

	debug().Msg("player: signal check")

	switch *statePtr {
	case StateDefault:
		s := <-sigChan

		debug().Msg("player: got signal before playing track")

		switch s {
		case SignalPlay:
			p.withMemory(func(m *PlayerMemory) {
				m.play(statePtr)
			})
		case SignalResume:
			if p.setNextTrackIndex(0) {
				*statePtr = StatePlaying
			}
		}
	case StateIdle:
		s := <-sigChan

		debug().Msg("player: got signal before playing track")

		switch s {
		case SignalPlay:
			p.withMemory(func(m *PlayerMemory) {
				m.play(statePtr)
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

		return
	}

	// TODO: if channel changes, then send pause + unpause signals ( if not currently paused )
	// to re-grab the voice channel

	// before playing the record again
	// sync-read the channel id
	c := p.voiceConnection()
	if c == nil {
		// TODO: log message
		panic("no voice connection")
	}

	err := func() error {

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

		// TODO: transcode on play handler
		// lightly transcode formats on the fly and send data packets to discord
		{
			opusEncoder, err := gopus.NewEncoder(SampleRate, NumChannels, gopus.Audio)
			if err != nil {
				return fmt.Errorf("faied to create opus encoder: %v", err)
			}

			inBufArray := [SampleSize * NumChannels]int16{}
			outPackets := [NumPacketBuffers][SampleMaxBytes]byte{}
			outPacketIdx := 0

			inBuf := inBufArray[:]

		BroadcastTrackLoop:
			for {

				noSignal := false

				select {
				case s := <-sigChan:
					switch s {
					case SignalPlay:
						p.withMemory(func(m *PlayerMemory) {
							m.play(statePtr)
						})
					case SignalPrevious:
						p.previousTrack(statePtr)
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
					case SignalPause:
						*statePtr = StatePaused
					PausedLoop:
						for {
							s := <-sigChan
							switch s {
							case SignalPlay:
								p.withMemory(func(m *PlayerMemory) {
									m.play(statePtr)
								})
							case SignalPrevious:
								p.previousTrack(statePtr)
								*statePtr = StateIdle
								break BroadcastTrackLoop
							case SignalNext:
								*statePtr = StateIdle
								break BroadcastTrackLoop
							case SignalStop:
								p.restartTrack()
								*statePtr = StateIdle
								break BroadcastTrackLoop
							case SignalReset:
								*statePtr = StateIdle
								p.reset()
								break BroadcastTrackLoop
							case SignalResume:
								c = p.voiceConnection()
								if c != nil {
									break PausedLoop
								}
								// TODO: log message: staying paused cuz there is no voice channel
							}
						}
					}
				default:
					noSignal = true
				}

				if !noSignal {
					debug().Msg("player: processed signal while playing")
				}

				err = binary.Read(f, binary.LittleEndian, &inBuf)
				if err != nil {
					if err == io.EOF || err == io.ErrUnexpectedEOF {
						break
					}
					return fmt.Errorf("faied to read from audio file: %s: %v", track.audioFile, err)
				}

				numBytes, err := opusEncoder.Encode(inBuf, SampleSize, outPackets[outPacketIdx][:])
				if err != nil {
					return fmt.Errorf("transcode error: %v", err)
				}

				if numBytes == 0 {
					break
				}

				c.OpusSend <- outPackets[outPacketIdx][:numBytes]

				outPacketIdx++
				if outPacketIdx >= NumPacketBuffers {
					outPacketIdx = 0
				}
			}
		}

		return nil
	}()
	if err != nil {
		log.Err(err).Msg("player: error occured during playback")
	}
}
