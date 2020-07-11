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

type TracedSignal struct {
	src interface{}
	sig Signal
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

type Track struct {
	// private
	audioFile string

	// public
	Url           string
	AuthorId      string
	AuthorMention string
}

type playRequest struct {
	track *Track
	patch bool
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

func (m *PlayerMemory) indexOfTrack(url string) int {

	for i, t := range m.tracks {
		if t.Url == url {
			return i
		}
	}

	return -1
}

func (m *PlayerMemory) play(s State) {

	r := <-m.playRequests

	if r.track == nil {
		return
	}

	t := *r.track

	if r.patch {
		// only changes the record value if a match can be found in the playlist
		// and it is clearly missing an audioFile
		//
		// does not attempt to insert it
		i := m.indexOfTrack(t.Url)
		if i >= 0 && m.tracks[i].audioFile == "" {
			m.tracks[i] = t
		}
	} else {

		switch s {
		case StateDefault:
			m.tracks = []Track{t}
			m.currentTrackIdx = -1
		case StateIdle:
			i := m.indexOfTrack(t.Url)
			if i == -1 {
				m.tracks = append(m.tracks, t)
			} else if t.audioFile != "" && m.tracks[i].audioFile == "" {
				// implicit patch
				m.tracks[i] = t
			}
			m.currentTrackIdx = len(m.tracks) - 2
		case StatePaused:
			i := m.indexOfTrack(t.Url)
			if i == -1 {
				m.tracks = append(m.tracks, t)
			} else if t.audioFile != "" && m.tracks[i].audioFile == "" {
				// implicit patch
				m.tracks[i] = t
			}
		case StatePlaying:
			i := m.indexOfTrack(t.Url)
			if i == -1 {
				m.tracks = append(m.tracks, t)
			} else if t.audioFile != "" && m.tracks[i].audioFile == "" {
				// implicit patch
				m.tracks[i] = t
			}
		}
	}
}

func (m *PlayerMemory) hasAudience(s *discordgo.Session, guildId string) bool {

	if m.voiceChannelId == "" {
		return false
	}

	g, err := s.Guild(guildId)
	if err != nil {
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
	mutex          sync.RWMutex
	memory         atomic.Value
	discordSession *discordgo.Session
	discordGuildId string

	stateMachine PlayerStateMachine
	signalChan   chan TracedSignal
}

func NewPlayer(s *discordgo.Session, guildId string) *Player {

	p := &Player{
		discordSession: s,
		discordGuildId: guildId,
		signalChan:     make(chan TracedSignal, 1),
		stateMachine: PlayerStateMachine{
			state:    StateDefault,
			niceness: NicenessMax,
		},
	}

	playRequests := make(chan *playRequest, 1)

	p.memory.Store(PlayerMemory{
		playRequests:    playRequests,
		currentTrackIdx: -1,
	})
	go p.playerGoroutine()

	return p
}

func (p *Player) notifyNoAudience(s Signal) {
	p.broadcastTextMessage("cannot process \"" + s.String() + "\" request at this time: there is no audience")
}

func (p *Player) nextTrack() *Track {
	var result *Track

	p.withMemory(func(m *PlayerMemory) {

		numCheckedTracks := 0

		for numCheckedTracks < len(m.tracks) {

			numCheckedTracks++
			m.currentTrackIdx++

			if m.currentTrackIdx >= len(m.tracks) {
				if m.notLooping {
					m.currentTrackIdx = -1
					return
				}
				m.currentTrackIdx = 0
			}

			if m.tracks[m.currentTrackIdx].audioFile != "" {
				track := m.tracks[m.currentTrackIdx]
				result = &track
				return
			}

			m.currentTrackIdx = -1
		}
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

func (p *Player) Play(srcEvt interface{}, url string, authorId, authorMention string, file string, patch bool) {
	p.withMemory(func(m *PlayerMemory) {

		m.playRequests <- &playRequest{
			track: &Track{
				audioFile:     file,
				Url:           url,
				AuthorId:      authorId,
				AuthorMention: authorMention,
			},
			patch: patch,
		}

		p.signalChan <- TracedSignal{srcEvt, SignalPlay}

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

func (p *Player) broadcastTextMessage(s string) {
	var c string

	p.withMemory(func(m *PlayerMemory) {
		c = m.textChannel
	})

	if c == "" {
		return
	}

	log.Debug().
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
		Interface("state", p.stateMachine.state).
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

func (p *Player) playerGoroutine() {

	defer func() {
		p.debug().Msg("player: permanently broken")
	}()

	p.debug().Msg("player: starting")

	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().
						Interface("error", r).
						Interface("state", p.stateMachine.state).
						Str("guild_id", p.discordGuildId).
						Msg("player: recovered from panic")
				}
			}()
			err := p.playerStateMachine()
			if err != nil {
				log.Err(err).Msg("player: error occured during playback")
			}
		}()
	}
}

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

		p.setDefaultTextChannel(s.src)

		p.debug().Msg("player: got signal before playing track")

		switch s.sig {
		case SignalNewVoiceConnection:
			sendChan = nil
		case SignalPlay:
			hasAudience := false

			p.withMemory(func(m *PlayerMemory) {
				m.play(p.stateMachine.state)
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

		p.debug().Msg("player: got signal before playing track")

		switch s.sig {
		case SignalNewVoiceConnection:
			sendChan = nil
		case SignalReset:
			p.reset()
			p.stateMachine.state = StateIdle
			return nil
		case SignalPlay:
			hasAudience := false

			p.withMemory(func(m *PlayerMemory) {
				m.play(p.stateMachine.state)
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
			p.withMemory(func(m *PlayerMemory) {
				m.currentTrackIdx -= 1
				if m.currentTrackIdx < -1 {
					m.currentTrackIdx = len(m.tracks) - 1
				}
			})
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

		msg := "now playing: " + track.Url
		if track.AuthorMention != "" {
			msg += " ( added by " + track.AuthorMention + " )"
		}

		p.broadcastTextMessage(msg)
	}

	err = p.setNiceness(NicenessNormal)
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

		// p.debug().Msg("player: broadcast loop start: signal check")

		select {
		// signal trap 3/4:
		// type: non-blocking
		// signals recognized when in playing state
		case s := <-p.signalChan:
			switch s.sig {
			case SignalNewVoiceConnection:
				sendChan = p.sendChannel()
				if sendChan == nil {
					return nil
				}
			case SignalPlay:
				p.withMemory(func(m *PlayerMemory) {
					m.play(p.stateMachine.state)
				})
			case SignalPrevious:
				p.previousTrack()
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
					switch s.sig {
					case SignalNewVoiceConnection:
						sendChan = nil
					case SignalPlay:
						p.withMemory(func(m *PlayerMemory) {
							m.play(p.stateMachine.state)
						})

						broadcastMsg := "player is paused; to resume playback send the following message:\n\n" +
							p.discordSession.State.User.Mention() + " resume"

						p.broadcastTextMessage(broadcastMsg)
					case SignalPrevious:
						p.previousTrack()
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
