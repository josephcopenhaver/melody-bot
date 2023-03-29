package service

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/josephcopenhaver/gopus"
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
	SignalDispose
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
		"dispose",
	}[int(s)]
}

type TracedSignal struct {
	src           interface{}
	sig           Signal
	signalPayload interface{}
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

type AudioStreamer interface {
	ReadCloser(context.Context, *sync.WaitGroup) (io.ReadCloser, error)
	SrcUrlStr() string
	Cached() bool
}

type Track struct {
	// public
	AudioStreamer
	AuthorId      string
	AuthorMention string
}

type playRequest struct {
	track *Track
}

type Playlist struct {
	Tracks          []Track
	CurrentTrackIdx int
}

type PlayerMemory struct {
	voiceChannelId  string
	voiceConnection *discordgo.VoiceConnection
	notLooping      bool
	currentTrackIdx int
	textChannel     string
	tracks          []Track
}

func (m *PlayerMemory) reset() {
	vc := m.voiceConnection

	*m = PlayerMemory{
		currentTrackIdx: -1,
	}

	if vc != nil {
		err := vc.Disconnect()
		if err != nil {
			log.Err(err).Msg("reset: failed to disconnect")
		}
	}
}

func (m *PlayerMemory) indexOfTrack(url string) int {

	for i, t := range m.tracks {
		if t.SrcUrlStr() == url {
			return i
		}
	}

	return -1
}

func (m *PlayerMemory) play(s State, r *playRequest, debug func() *zerolog.Event) {

	if r == nil {
		debug().Msg("no playRequest in play handler?")
		return
	}

	if r.track == nil {
		debug().Msg("no track in play handler?")
		return
	}

	t := *r.track

	debug().
		Str("state", s.String()).
		Int("current_track_idx", m.currentTrackIdx).
		Int("num_tracks", len(m.tracks)).
		Str("new_track_url", t.SrcUrlStr()).
		Msg("play track")

	switch s {
	case StateDefault:
		m.tracks = []Track{t}
		m.currentTrackIdx = 0
	case StateIdle:
		i := m.indexOfTrack(t.SrcUrlStr())
		if i == -1 {
			m.tracks = append(m.tracks, t)
			m.currentTrackIdx = len(m.tracks) - 1
		}
		// TODO: else if already in track list, then consider moving track to end or moving the currentTrackIdx
	case StatePaused:
		fallthrough
	case StatePlaying:
		i := m.indexOfTrack(t.SrcUrlStr())
		if i == -1 {
			m.tracks = append(m.tracks, t)
		}
	}
}

func (m *PlayerMemory) hasAudience(s *discordgo.Session, guildId string) bool {

	if m.voiceChannelId == "" {
		return false
	}

	g, err := s.State.Guild(guildId)
	if err != nil || g == nil {
		log.Err(err).Msg("player: hasAudience: failed to get guild voice states")
		return false
	}

	// short circuit if there is an audience
	for _, v := range g.VoiceStates {

		if v.ChannelID != m.voiceChannelId {
			continue
		}

		// ignore my own status
		if v.UserID == s.State.User.ID {
			continue
		}

		if v.Deaf || v.SelfDeaf {
			continue
		}

		// ignore users that are bots
		u, err := s.User(v.UserID)
		if err != nil {
			log.Err(err).Msg("failed to get user info, assuming it is a bot")
			continue
		} else if u.Bot {
			continue
		}

		return true
	}

	return false
}

type PlayerStateMachine struct {
	state    State
	niceness int
}

type Player struct {
	ctx            context.Context
	wg             *sync.WaitGroup
	mutex          sync.RWMutex
	memory         atomic.Value
	discordSession *discordgo.Session
	discordGuildId string

	stateMachine PlayerStateMachine
	signalChan   chan TracedSignal
}

func NewPlayer(ctx context.Context, wg *sync.WaitGroup, s *discordgo.Session, guildId string) *Player {

	p := &Player{
		ctx:            ctx,
		wg:             wg,
		discordSession: s,
		discordGuildId: guildId,
		signalChan:     make(chan TracedSignal, 1),
		stateMachine: PlayerStateMachine{
			state:    StateDefault,
			niceness: NicenessMax,
		},
	}

	p.memory.Store(PlayerMemory{
		currentTrackIdx: -1,
	})

	wg.Add(1)
	go p.playerGoroutine(wg)

	return p
}

func (p *Player) notifyNoAudience(s Signal) {
	p.broadcastTextMessage("cannot process \"" + s.String() + "\" request at this time: there is no audience")
}

func (p *Player) nextTrack() *Track {
	var result *Track

	p.withMemory(func(m *PlayerMemory) {

		if len(m.tracks) == 0 {
			p.debug().Msg("next track called when there is no track list: playback stopping")
			m.currentTrackIdx = -1
			return
		}

		m.currentTrackIdx++

		if m.currentTrackIdx >= len(m.tracks) {
			if m.notLooping {
				m.currentTrackIdx = -1
				return
			}
			m.currentTrackIdx = 0
		}

		track := m.tracks[m.currentTrackIdx]
		result = &track
	})

	return result
}

func (p *Player) withMemoryErr(f func(m *PlayerMemory) error) error {

	// p.debug().Msg("withMemory: waiting for lock")

	p.mutex.Lock()
	defer p.mutex.Unlock()

	// defer p.debug().Msg("withMemory: releasing lock")
	// p.debug().Msg("withMemory: got lock")

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

	p.signalChan <- TracedSignal{srcEvt, SignalReset, nil}
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

func (p *Player) previousTrack(offset int) {
	p.withMemory(func(m *PlayerMemory) {
		if len(m.tracks) < 2 {
			m.currentTrackIdx = -1
			return
		}

		if m.currentTrackIdx < 0 {
			m.currentTrackIdx = len(m.tracks) - 1
		}

		offset++

		for offset > 0 {
			offset--

			m.currentTrackIdx -= 1
			if m.currentTrackIdx < 0 {
				m.currentTrackIdx = len(m.tracks) - 1
			}
		}
	})
}

func (p *Player) Pause(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalPause, nil}
}

func (p *Player) Stop(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalStop, nil}
}

func (p *Player) Resume(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalResume, nil}
}

func (p *Player) Next(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalNext, nil}
}

func (p *Player) Previous(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalPrevious, nil}
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

func (p *Player) Play(srcEvt interface{}, authorId, authorMention string, as AudioStreamer) {
	// TODO: pool
	payload := &playRequest{
		track: &Track{
			AudioStreamer: as,
			AuthorId:      authorId,
			AuthorMention: authorMention,
		},
	}

	p.withMemory(func(m *PlayerMemory) {
		p.signalChan <- TracedSignal{srcEvt, SignalPlay, payload}

	})
}

func (p *Player) SetVoiceConnection(srcEvt interface{}, channelId string, c *discordgo.VoiceConnection) {

	p.withMemory(func(m *PlayerMemory) {

		if c == nil && m.voiceConnection != nil {
			err := m.voiceConnection.Disconnect()
			if err != nil {
				log.Err(err).Msg("SetVoiceConnection: failed to disconnect")
			}
		}

		m.voiceChannelId = channelId
		m.voiceConnection = c
	})

	p.signalChan <- TracedSignal{srcEvt, SignalNewVoiceConnection, nil}
}

func (p *Player) ClearPlaylist(srcEvt interface{}) {

	p.withMemory(func(m *PlayerMemory) {

		m.tracks = nil
	})
}

func (p *Player) RestartTrack(srcEvt interface{}) {

	p.signalChan <- TracedSignal{srcEvt, SignalRestartTrack, nil}
}

func (p *Player) SetTextChannel(s string) {
	p.withMemory(func(m *PlayerMemory) {
		m.textChannel = s
	})

	p.broadcastTextMessage("text channel is now this one")
}

func (p *Player) GetVoiceChannelId() string {
	var result string

	p.withMemory(func(m *PlayerMemory) {
		result = m.voiceChannelId
	})

	return result
}

func (p *Player) HasAudience() bool {
	result := false

	p.withMemory(func(m *PlayerMemory) {
		result = m.hasAudience(p.discordSession, p.discordGuildId)
	})

	return result
}

func (p *Player) RemoveTrack(url string) bool {
	result := false

	p.withMemory(func(m *PlayerMemory) {

		i := m.indexOfTrack(url)
		if i == -1 {
			return
		}

		result = true

		if len(m.tracks) <= 1 {
			m.tracks = nil
			m.currentTrackIdx = -1

			return
		}

		copy(m.tracks[i:], m.tracks[i+1:])
		m.tracks = m.tracks[:len(m.tracks)-1]

		if m.currentTrackIdx >= i {
			m.currentTrackIdx -= 1
		}
	})

	return result
}

func (p *Player) GetPlaylist() Playlist {
	var result Playlist

	p.withMemory(func(m *PlayerMemory) {

		if len(m.tracks) == 0 {
			return
		}

		result.Tracks = make([]Track, len(m.tracks))
		copy(result.Tracks, m.tracks)

		result.CurrentTrackIdx = m.currentTrackIdx
	})

	if len(result.Tracks) == 0 {
		result.CurrentTrackIdx = -1
	} else if result.CurrentTrackIdx >= len(result.Tracks) {
		result.CurrentTrackIdx = 0
	}

	return result
}

func (p *Player) setDefaultTextChannel(s Signal, v interface{}) {

	switch e := v.(type) {
	case *discordgo.MessageCreate:
		if e == nil {
			return
		}

		var changed bool
		p.withMemory(func(m *PlayerMemory) {
			if m.textChannel == "" {
				changed = true
				m.textChannel = e.Message.ChannelID
			}
		})

		if changed {
			p.broadcastTextMessage("text channel is now this one")
		}
	}
}

func (p *Player) broadcastTextMessage(s string) {
	var c string

	p.withMemory(func(m *PlayerMemory) {
		c = m.textChannel
	})

	if c == "" {
		return
	}

	p.debug().
		Str("guild_id", p.discordGuildId).
		Str("notification_message", s).
		Msg("broadcasting notification")

	_, err := p.discordSession.ChannelMessageSend(c, s)
	if err != nil {
		log.Err(err).
			Str("notification_message", s).
			Msg("failed to send message")
	}
}

func (p *Player) sendChannel() chan<- []byte {
	var c *discordgo.VoiceConnection
	var result chan<- []byte

	p.withMemory(func(m *PlayerMemory) {
		c = m.voiceConnection
	})

	if c == nil {
		p.broadcastTextMessage("no active voice channel")
		return result
	}

	c.Lock()
	defer c.Unlock()

	if !c.Ready {
		p.broadcastTextMessage("voice channel not ready")
		return result
	}

	result = c.OpusSend

	if result == nil {
		p.broadcastTextMessage("voice channel has no sender")
	}

	return result
}

func (p *Player) debug() *zerolog.Event {
	return log.Debug().
		Str("state", p.stateMachine.state.String()).
		Str("guild_id", p.discordGuildId)
}

func (p *Player) setNiceness(n int) error {

	if p.stateMachine.niceness == n {

		return nil
	}

	err := SetNiceness(n)
	if err != nil {
		return err
	}

	p.stateMachine.niceness = n

	return nil
}

func (p *Player) playerGoroutine(wg *sync.WaitGroup) {
	defer wg.Done()

	defer func() {
		if p.ctx.Err() == nil {
			p.debug().Msg("player: permanently broken") // should never happen
		}
	}()

	p.debug().Msg("player: starting")

	doneChan := p.ctx.Done()
	go func() {
		<-doneChan
		p.signalChan <- TracedSignal{nil, SignalDispose, nil}
	}()

	var done bool
	var errCount int
	for !done {
		func() {
			defer func() {
				if r := recover(); r != nil {
					prevState := p.stateMachine.state
					p.stateMachine = PlayerStateMachine{
						state:    StateDefault,
						niceness: NicenessMax,
					}
					p.reset()
					evt := log.Error()
					if e, ok := r.(error); ok {
						evt = evt.Err(e)
					} else {
						evt = evt.Interface("error", r)
					}
					evt.Str("state", prevState.String()).
						Str("guild_id", p.discordGuildId).
						Msg("player: recovered from panic")
				}
			}()
			err := p.playerStateMachine()
			if err != nil {
				if err == ErrDisposed {
					done = true
					return
				}
				log.Err(err).Msg("player: error occurred during playback")

				errCount++
				var numTracks int
				p.withMemory(func(m *PlayerMemory) {
					numTracks = len(m.tracks)
				})
				if errCount >= numTracks {
					// TODO: make this a stop action instead
					log.Err(err).Msg("player: too many errors occurred, resetting playback")

					p.reset()
				}
			} else {
				errCount = 0
			}
		}()
	}

	p.reset()
}

var ErrDisposed = errors.New("player disposed")

func (p *Player) playerStateMachine() error {
	var err error
	var sendChan chan<- []byte

	p.debug().Msg("player: main loop start: signal check")

	switch p.stateMachine.state {
	case StateDefault:
		// signal trap 1/4:
		// type: blocking
		// signals recognized when in initial state
		s := <-p.signalChan

		p.setDefaultTextChannel(s.sig, s.src)

		p.debug().Msg("player: got signal before playing track")

		switch s.sig {
		case SignalDispose:
			return ErrDisposed
		case SignalNewVoiceConnection:
			sendChan = nil
		case SignalPlay:
			hasAudience := false

			p.withMemory(func(m *PlayerMemory) {
				m.play(p.stateMachine.state, s.signalPayload.(*playRequest), p.debug)
				hasAudience = m.hasAudience(p.discordSession, p.discordGuildId)
			})

			if !hasAudience {
				p.notifyNoAudience(s.sig)
				p.stateMachine.state = StateIdle
				return nil
			}

			p.stateMachine.state = StatePlaying
		}
	case StateIdle:
		err = p.setNiceness(NicenessMax)
		if err != nil {
			return err
		}

		// signal trap 2/4:
		// type: blocking
		// signals recognized when in idle state ( stopped or partially errored )
		s := <-p.signalChan

		p.setDefaultTextChannel(s.sig, s.src)

		p.debug().Msg("player: got signal before playing track")

		switch s.sig {
		case SignalDispose:
			return ErrDisposed
		case SignalNewVoiceConnection:
			sendChan = nil
		case SignalReset:
			p.reset()
			p.stateMachine.state = StateIdle
			return nil
		case SignalPlay:
			hasAudience := false

			p.withMemory(func(m *PlayerMemory) {
				m.play(p.stateMachine.state, s.signalPayload.(*playRequest), p.debug)
				hasAudience = m.hasAudience(p.discordSession, p.discordGuildId)
			})

			if !hasAudience {
				p.notifyNoAudience(s.sig)
				return nil
			}

			p.stateMachine.state = StatePlaying
		case SignalResume:
			p.stateMachine.state = StatePlaying
		case SignalNext:
			// do nothing, let loop normally advance
		case SignalPrevious:
			p.previousTrack(0) // nothing is currently playing
		}
	}

	p.debug().Msg("player: play check")

	if p.stateMachine.state != StatePlaying {

		p.debug().Msg("player: not playing")

		p.stateMachine.state = StateIdle

		return nil
	}

	if sendChan == nil {
		sendChan = p.sendChannel()

		if sendChan == nil {

			p.debug().Msg("player: trying to play, but no broadcast channel is ready")

			p.stateMachine.state = StateIdle

			return nil
		}
	}

	track := p.nextTrack()
	if track == nil {
		p.stateMachine.state = StateIdle
		return nil
	} else {

		msg := "now playing: " + track.SrcUrlStr()
		if track.AuthorMention != "" {
			msg += " ( added by " + track.AuthorMention + " )"
		}

		p.broadcastTextMessage(msg)
	}

	err = p.setNiceness(NicenessNormal)
	if err != nil {
		return err
	}

	pctx := p.ctx
	if pctx.Err() != nil {
		return nil
	}

	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()

	f, err := track.ReadCloser(ctx, p.wg)
	if err != nil {
		return fmt.Errorf("failed to open audio stream: %s, %t: %v", track.SrcUrlStr(), track.Cached(), err)
	}
	defer f.Close()

	//
	// read packets from file and buffer them to send to broadcast channel
	//

	br := bufio.NewReaderSize(f, SampleMaxBytes)

	opusEncoder, err := gopus.NewEncoder(SampleRate, NumChannels, gopus.Audio)
	if err != nil {
		return err
	}

	pcmBuf := [SampleSize]int16{}

	for {

		noSignal := false

		// p.debug().Msg("player: broadcast loop start: signal check")

		select {
		// signal trap 3/4:
		// type: non-blocking
		// signals recognized when in playing state
		case s := <-p.signalChan:
			p.setDefaultTextChannel(s.sig, s.src)

			switch s.sig {
			case SignalDispose:
				return ErrDisposed
			case SignalNewVoiceConnection:
				sendChan = p.sendChannel()
				if sendChan == nil {
					return nil
				}
			case SignalPlay:
				p.withMemory(func(m *PlayerMemory) {
					m.play(p.stateMachine.state, s.signalPayload.(*playRequest), p.debug)
				})
			case SignalPrevious:
				p.previousTrack(1) // current track is playing
				return nil
			case SignalStop:
				p.restartTrack()
				p.stateMachine.state = StateIdle
				return nil
			case SignalReset:
				p.reset()
				p.stateMachine.state = StateIdle
				return nil
			case SignalNext:
				return nil
			case SignalRestartTrack:
				p.restartTrack()
				return nil
			case SignalPause:
				p.stateMachine.state = StatePaused

				err = p.setNiceness(NicenessMax)
				if err != nil {
					return err
				}

			PausedLoop:
				for {
					// signal trap 4/4:
					// type: blocking
					// signals recognized when in paused state
					s := <-p.signalChan

					p.setDefaultTextChannel(s.sig, s.src)

					switch s.sig {
					case SignalDispose:
						return ErrDisposed
					case SignalNewVoiceConnection:
						sendChan = nil
					case SignalPlay:
						p.withMemory(func(m *PlayerMemory) {
							m.play(p.stateMachine.state, s.signalPayload.(*playRequest), p.debug)
						})

						broadcastMsg := "player is paused; to resume playback send the following message:\n\n" +
							p.discordSession.State.User.Mention() + " resume"

						p.broadcastTextMessage(broadcastMsg)
					case SignalPrevious:
						p.previousTrack(1) // current track is paused
						p.stateMachine.state = StateIdle
						return nil
					case SignalNext:
						p.stateMachine.state = StateIdle
						return nil
					case SignalStop:
						p.restartTrack()
						p.stateMachine.state = StateIdle
						return nil
					case SignalRestartTrack:
						p.restartTrack()
						p.stateMachine.state = StateIdle
						return nil
					case SignalReset:
						p.reset()
						p.stateMachine.state = StateIdle
						return nil
					case SignalResume:
						p.stateMachine.state = StatePlaying
						break PausedLoop
					}
				}

				err = p.setNiceness(NicenessNormal)
				if err != nil {
					return err
				}

				// rediscover the channel we need to send on
				// if it was altered while paused
				if sendChan == nil {
					sendChan = p.sendChannel()
					if sendChan == nil {
						return nil
					}
				}
			}
		default:
			noSignal = true
		}

		if !noSignal {
			p.debug().Msg("player: processed signal while playing")
		}

		// TODO: modify discordgo to support a packet pool
		packet := make([]byte, SampleMaxBytes)

		if pctx.Err() != nil {
			return ErrDisposed
		}

		err = binary.Read(br, binary.LittleEndian, &pcmBuf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {

				if flushable, ok := f.(interface{ Flushed() bool }); ok {
					p.debug().Msg("flushing track")
					if flushable.Flushed() {
						p.debug().Msg("flush: true")
						return nil
					}
					p.debug().Msg("flush: false")
				}

				p.debug().Err(err).Msg("file read interrupted")

				return nil
			}
			return fmt.Errorf("error reading track: %s: %v", track.SrcUrlStr(), err)
		}

		numBytes, err := opusEncoder.Encode(pcmBuf[:], SampleSize, packet)
		if numBytes == 0 {
			if err == nil {
				p.debug().Msg("opus encode created zero bytes")
				return nil
			}

			return err
		}

		if pctx.Err() != nil {
			return ErrDisposed
		}

		// TODO: modify discordgo to support a packet pool

		sendChan <- packet[:numBytes]

		if pctx.Err() != nil {
			return ErrDisposed
		}

		if err != nil {
			return fmt.Errorf("error encoding track to opus: %s: %v", track.SrcUrlStr(), err)
		}
	}
}
