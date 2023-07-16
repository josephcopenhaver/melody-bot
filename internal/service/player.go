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
	"time"

	"github.com/josephcopenhaver/gopus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

type PlaylistID struct {
	uuid.UUID
}

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
	PlaylistID() string
	PlayerStateLastChangedAt() time.Time
}

type Track struct {
	// public
	AudioStreamer
	AuthorId      string
	AuthorMention string
}

type playRequest struct {
	track      *Track
	playlistID string
	pslc       time.Time
}

type Playlist struct {
	Tracks          []Track
	CurrentTrackIdx int
}

type PlayerMemory struct {
	id              uuid.UUID
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
		id:              uuid.New(),
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
		m.tracks = append(m.tracks, t)
		m.currentTrackIdx = 0
	case StateIdle:
		i := m.indexOfTrack(t.SrcUrlStr())
		if i == -1 {
			m.tracks = append(m.tracks, t)
			m.currentTrackIdx = len(m.tracks) - 1
		}
		// TODO: else if already in track list, then consider moving track to end or moving the currentTrackIdx
	case StatePaused, StatePlaying:
		i := m.indexOfTrack(t.SrcUrlStr())
		if i == -1 {
			m.tracks = append(m.tracks, t)
		}
	}
}

// hasAudience is broken in latest release of discord-go
func (m *PlayerMemory) hasAudience(s *discordgo.Session, guildId string) bool {

	if m.voiceChannelId == "" {
		log.Error().Msg("player: no voice channel id set, assuming no audience")
		return false
	}

	g, err := s.State.Guild(guildId)
	if err != nil || g == nil {
		log.Err(err).Msg("player: hasAudience: failed to get guild voice states, assuming no audience")
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

	log.Error().
		Int("voice_state_count", len(g.VoiceStates)).
		Str("guild_id", guildId).
		Msg("player: found no human voice states")

	return false
}

type PlayerStateMachine struct {
	rwMutex            *sync.RWMutex
	createdAt          time.Time
	stateLastChangedAt time.Time
	state              State
}

func newPlayerStateMachine(rwm *sync.RWMutex) PlayerStateMachine {
	now := time.Now()
	if rwm == nil {
		rwm = &sync.RWMutex{}
	}
	return PlayerStateMachine{
		rwMutex:            rwm,
		createdAt:          now,
		stateLastChangedAt: now,
		state:              StateDefault,
	}
}

type PlayCall struct {
	MessageCreate *discordgo.MessageCreate
	AuthorID      string
	AuthorMention string
	AudioStreamer AudioStreamer
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
	cancelMutex  sync.Mutex
	cancelFuncs  map[*func(error)]struct{}
	playPacks    chan (<-chan PlayCall)
}

func NewPlayer(ctx context.Context, wg *sync.WaitGroup, s *discordgo.Session, guildId string) *Player {

	p := &Player{
		ctx:            ctx,
		wg:             wg,
		discordSession: s,
		discordGuildId: guildId,
		signalChan:     make(chan TracedSignal, 1),
		stateMachine:   newPlayerStateMachine(nil),
		cancelFuncs:    map[*func(error)]struct{}{},
		playPacks:      make(chan (<-chan PlayCall)),
	}

	p.memory.Store(PlayerMemory{
		id:              uuid.New(),
		currentTrackIdx: -1,
	})

	wg.Add(1)
	go p.playerGoroutine(wg)
	wg.Add(1)
	go p.playPackGoroutine(wg)

	return p
}

func (p *Player) setState(s State) {
	sm := &p.stateMachine

	sm.rwMutex.RLock()
	cleanup := sm.rwMutex.RUnlock
	defer func() {
		if f := cleanup; f != nil {
			cleanup = nil
			f()
		}
	}()

	oldState := sm.state
	if s == oldState {
		return
	}

	if f := cleanup; f != nil {
		cleanup = nil
		f()
	}

	sm.rwMutex.Lock()
	cleanup = sm.rwMutex.Unlock

	// redo read condition check as state may have changed when lock was released and re-acquired for W mode
	oldState = sm.state
	if s == oldState {
		return
	}

	sm.state = s
	now := time.Now()

	if oldState == StateDefault {
		return
	}

	sm.stateLastChangedAt = now
}

func (p *Player) RegisterCanceler(fp *func(error)) {
	if fp == nil {
		return
	}

	p.withCancelLock(func(m map[*func(error)]struct{}) {
		m[fp] = struct{}{}
	})
}

func (p *Player) DeregisterCanceler(fp *func(error)) {
	if fp == nil {
		return
	}

	p.withCancelLock(func(m map[*func(error)]struct{}) {
		delete(m, fp)
	})
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

func (p *Player) PlaylistID() PlaylistID {
	var result PlaylistID

	p.withMemory(func(m *PlayerMemory) {
		result = PlaylistID{m.id}
	})

	return result
}

func (p *Player) StateLastChangedAt() time.Time {
	p.stateMachine.rwMutex.RLock()
	defer p.stateMachine.rwMutex.RUnlock()
	return p.stateMachine.stateLastChangedAt
}

func (p *Player) stateUnchangedSince(t time.Time) bool {
	return p.stateMachine.stateLastChangedAt.Compare(t) == 0
}

func (p *Player) withCancelLock(f func(m map[*func(error)]struct{})) {

	p.cancelMutex.Lock()
	defer p.cancelMutex.Unlock()

	f(p.cancelFuncs)
}

// withMemoryErr should only be used to modify state of the player safely
//
// do not mix it with other responsibilities like send to or receiving from channels
// you'll likely encounter deadlocks
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
		log.Err(err).Msg("error interacting with player memory")
		return err
	}

	p.memory.Store(m)

	return nil
}

// withMemory should only be used to modify state of the player safely
//
// do not mix it with other responsibilities like send to or receiving from channels
// you'll likely encounter deadlocks
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

	// cancel all old async contexts for the previous playlist id
	{
		var oldMap map[*func(error)]struct{}
		p.withCancelLock(func(m map[*func(error)]struct{}) {
			oldMap = m
			p.cancelFuncs = map[*func(error)]struct{}{}
		})

		if len(oldMap) > 0 {
			err := context.Canceled

			var wg sync.WaitGroup
			wg.Add(len(oldMap))
			for k := range oldMap {
				k := k

				go func() {
					defer wg.Done()

					(*k)(err)
				}()
			}

			// wait for all cancels to finish
			wg.Wait()
		}
	}
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
		Str("notification_message", s).
		Msg("broadcasting notification")

	_, err := p.discordSession.ChannelMessageSend(c, s)
	if err != nil {
		log.Err(err).
			Str("notification_message", s).
			Msg("failed to send message")
	}
}

func (p *Player) BroadcastTextMessage(s string) {
	p.broadcastTextMessage(s)
}

func (p *Player) sendChannel() chan<- []byte {
	var vc *discordgo.VoiceConnection
	var result chan<- []byte

	p.withMemory(func(m *PlayerMemory) {
		vc = m.voiceConnection
	})

	if vc == nil {
		p.broadcastTextMessage("no active voice channel")
		return result
	}

	sendChan, ok := func() (chan<- []byte, bool) {
		vc.RLock()
		defer vc.RUnlock()

		return vc.OpusSend, vc.Ready
	}()

	if !ok {
		p.broadcastTextMessage("voice channel not ready")
		return result
	}

	if sendChan == nil {
		p.broadcastTextMessage("voice channel has no sender")
	}

	result = sendChan

	return result
}

func (p *Player) debug() *zerolog.Event {
	return log.Debug().
		Str("state", p.stateMachine.state.String()).
		Str("guild_id", p.discordGuildId)
}

func (p *Player) Enqueue(playPack <-chan PlayCall) (result bool) {
	defer func() {
		// TODO: unhackify this in some fashion
		// should never attempt to offer to the channel if it is closed
		if r := recover(); r != nil {
			result = false
		}
	}()

	p.playPacks <- playPack

	return true
}

func (p *Player) playPackGoroutine(wg *sync.WaitGroup) {
	defer wg.Done()
	defer close(p.playPacks)

	doneChan := p.ctx.Done()

	var done bool
	for !done {
		select {
		case <-doneChan:
			done = true
			continue
		default:
		}

		select {
		case <-doneChan:
			done = true
			continue
		case playPackChan, ok := <-p.playPacks:
			if !ok {
				done = true
				continue
			}

			for v := range playPackChan {
				as := v.AudioStreamer

				// TODO: pool
				payload := &playRequest{
					track: &Track{
						AudioStreamer: as,
						AuthorId:      v.AuthorID,
						AuthorMention: v.AuthorMention,
					},
					playlistID: as.PlaylistID(),
					pslc:       as.PlayerStateLastChangedAt(),
				}

				var ctxExpired bool
				p.withMemory(func(m *PlayerMemory) {
					if as.PlaylistID() != m.id.String() {
						p.debug().Msg("context is expired")
						// context is expired and is no longer valid
						ctxExpired = true
					}

				})

				if !ctxExpired {
					p.signalChan <- TracedSignal{v.MessageCreate, SignalPlay, payload}
				}
			}
		}
	}
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
					p.stateMachine = newPlayerStateMachine(p.stateMachine.rwMutex)
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

	p.debug().Str("state", p.stateMachine.state.String()).Msg("player: main loop start: signal check")

	switch p.stateMachine.state {
	case StateDefault:
		// signal trap 1/4:
		// type: blocking
		// signals recognized when in initial state
		s := <-p.signalChan

		p.debug().Str("signal", s.sig.String()).Msg("player: got signal before playing track")

		p.setDefaultTextChannel(s.sig, s.src)

		switch s.sig {
		case SignalDispose:
			return ErrDisposed
		case SignalNewVoiceConnection:
			sendChan = nil
		case SignalPlay:
			hasAudience := false

			pr := s.signalPayload.(*playRequest)
			isSyncCall := p.stateUnchangedSince(pr.pslc)

			var ctxExpired bool
			p.withMemory(func(m *PlayerMemory) {
				if pr.playlistID != m.id.String() {
					p.debug().Msg("context is expired")
					ctxExpired = true
					return
				}

				m.play(p.stateMachine.state, pr, p.debug)
				if isSyncCall {
					hasAudience = m.hasAudience(p.discordSession, p.discordGuildId)
				}
			})

			if ctxExpired {
				// ignore signal, don't change any state
				return nil
			}

			if isSyncCall {

				if !hasAudience {
					p.notifyNoAudience(s.sig)
					p.setState(StateIdle)
					return nil
				}

				p.setState(StatePlaying)
			} else {
				p.setState(StateIdle)
			}
		}
	case StateIdle:

		// signal trap 2/4:
		// type: blocking
		// signals recognized when in idle state ( stopped or partially errored )
		s := <-p.signalChan

		p.debug().Str("signal", s.sig.String()).Msg("player: got signal before playing track")

		p.setDefaultTextChannel(s.sig, s.src)

		p.debug().Msg("player: default text channel set")

		switch s.sig {
		case SignalDispose:
			return ErrDisposed
		case SignalNewVoiceConnection:
			sendChan = nil
		case SignalReset:
			p.reset()
			p.setState(StateIdle)
			return nil
		case SignalPlay:
			hasAudience := false

			pr := s.signalPayload.(*playRequest)
			isSyncCall := p.stateUnchangedSince(pr.pslc)

			var ctxExpired bool
			p.withMemory(func(m *PlayerMemory) {
				if pr.playlistID != m.id.String() {
					p.debug().Msg("context is expired")
					ctxExpired = true
					return
				}

				m.play(p.stateMachine.state, pr, p.debug)
				if isSyncCall {
					hasAudience = m.hasAudience(p.discordSession, p.discordGuildId)
				}
			})

			if ctxExpired {
				// ignore signal, don't change any state
				return nil
			}

			if isSyncCall {
				if !hasAudience {
					p.notifyNoAudience(s.sig)
					return nil
				}

				p.setState(StatePlaying)
			}
			// else stay in the idle state
		case SignalResume:
			p.setState(StatePlaying)
		case SignalNext:
			// do nothing, let loop normally advance
		case SignalPrevious:
			p.previousTrack(0) // nothing is currently playing
		}
	}

	p.debug().Msg("player: play check")

	if p.stateMachine.state != StatePlaying {

		p.debug().Str("state", p.stateMachine.state.String()).Msg("player: not playing")

		p.setState(StateIdle)

		return nil
	}

	if sendChan == nil {
		sendChan = p.sendChannel()

		if sendChan == nil {

			p.debug().Msg("player: trying to play, but no broadcast channel is ready")

			p.setState(StateIdle)

			return nil
		}
	}

	track := p.nextTrack()
	if track == nil {
		p.setState(StateIdle)
		return nil
	} else {

		msg := "now playing: " + track.SrcUrlStr()
		if track.AuthorMention != "" {
			msg += " ( added by " + track.AuthorMention + " )"
		}

		p.broadcastTextMessage(msg)
	}

	pctx := p.ctx
	if pctx.Err() != nil {
		return nil
	}

	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()

	f, err := track.ReadCloser(ctx, p.wg)
	if err != nil {
		return fmt.Errorf("failed to open audio stream: %s, %t: %w", track.SrcUrlStr(), track.Cached(), err)
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
			p.debug().Str("signal", s.sig.String()).Msg("player: got signal while playing")
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
				pr := s.signalPayload.(*playRequest)

				var ctxExpired bool
				p.withMemory(func(m *PlayerMemory) {
					if pr.playlistID != m.id.String() {
						p.debug().Msg("context is expired")
						ctxExpired = true
						return
					}

					m.play(p.stateMachine.state, pr, p.debug)
				})

				if ctxExpired {
					// ignore signal, don't change any state, continue playback
					continue
				}
			case SignalPrevious:
				p.previousTrack(1) // current track is playing
				return nil
			case SignalStop:
				p.restartTrack()
				p.setState(StateIdle)
				return nil
			case SignalReset:
				p.reset()
				p.setState(StateIdle)
				return nil
			case SignalNext:
				return nil
			case SignalRestartTrack:
				p.restartTrack()
				return nil
			case SignalPause:
				p.setState(StatePaused)

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

						pr := s.signalPayload.(*playRequest)
						isSyncCall := p.stateUnchangedSince(pr.pslc)

						var ctxExpired bool
						p.withMemory(func(m *PlayerMemory) {
							if pr.playlistID != m.id.String() {
								p.debug().Msg("context is expired")
								ctxExpired = true
								return
							}

							m.play(p.stateMachine.state, pr, p.debug)
						})

						if ctxExpired {
							// ignore signal, don't change any state, stay paused
							continue
						}

						if isSyncCall {
							broadcastMsg := "player is paused; to resume playback send the following message:\n\n" +
								p.discordSession.State.User.Mention() + " resume"

							p.broadcastTextMessage(broadcastMsg)
						}
					case SignalPrevious:
						p.previousTrack(1) // current track is paused
						p.setState(StateIdle)
						return nil
					case SignalNext:
						p.setState(StateIdle)
						return nil
					case SignalStop:
						p.restartTrack()
						p.setState(StateIdle)
						return nil
					case SignalRestartTrack:
						p.restartTrack()
						p.setState(StateIdle)
						return nil
					case SignalReset:
						p.reset()
						p.setState(StateIdle)
						return nil
					case SignalResume:
						p.setState(StatePlaying)
						break PausedLoop
					}
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
			return fmt.Errorf("error reading track: %s: %w", track.SrcUrlStr(), err)
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
			return fmt.Errorf("error encoding track to opus: %s: %w", track.SrcUrlStr(), err)
		}
	}
}
