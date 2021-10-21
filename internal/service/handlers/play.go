package handlers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/bwmarrin/discordgo"
	"github.com/kkdai/youtube/v2"

	"github.com/josephcopenhaver/melody-bot/internal/service"
)

const (
	AudioFileName = "audio.discord-opus"
)

// TODO: handle voice channel reconnects forced by the server, specifically when forced into a channel where no one is present

func Play() HandleMessageCreate {

	return newHandleMessageCreate(
		"play",
		"play <url>",
		"append track from youtube url to the playlist",
		newRegexMatcher(
			true,
			regexp.MustCompile(`^\s*play\s+(?P<url>[^\s]+.*?)\s*$`),
			playAfterTranscode,
		),
	)
}

type audioStream struct {
	size int64
	*youtube.Video
	*youtube.Format
	httpClient  *http.Client
	dstFilePath string
}

type readCloser struct {
	read  func([]byte) (int, error)
	close func() error
}

type countingReader struct {
	reader    io.Reader
	bytesRead int64
}

func newCountingReader(r io.Reader) *countingReader {
	return &countingReader{
		reader: r,
	}
}

func (r *countingReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	r.bytesRead += int64(n)
	return n, err
}

func (rc *readCloser) Read(p []byte) (int, error) {
	return rc.read(p)
}

func (rc *readCloser) Close() error {
	return rc.close()
}

func (as *audioStream) Cached() bool {

	_, err := os.Stat(as.dstFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Err(err).
				Msg("failed to stat file system")
			return false
		}

		return false
	}

	return true
}

func (as *audioStream) ReadCloser(ctx context.Context, wg *sync.WaitGroup) (io.ReadCloser, error) {

	if as.Cached() {
		return os.Open(as.dstFilePath)
	}

	var errResp struct {
		sync.RWMutex
		err error
	}

	setErr := func(err error) {
		if err == nil {
			return
		}

		errResp.Lock()
		defer errResp.Unlock()

		if errResp.err != nil {
			return
		}

		errResp.err = err
	}

	getErr := func() error {
		errResp.RLock()
		defer errResp.RUnlock()

		return errResp.err
	}

	successWaitCtx, successWaitCancel := context.WithCancel(context.Background())
	defer successWaitCancel()

	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(ctx)
	result := readCloser{
		close: func() error {
			defer successWaitCancel()
			defer cancel()
			pr.Close()
			return nil
		},
	}

	wg.Add(1)
	go func() {
		defer successWaitCancel()
		defer wg.Done()

		defer pw.Close()

		bw := bufio.NewWriter(pw)

		var dlc youtube.Client
		dlc.HTTPClient = as.httpClient

		f, s, err := dlc.GetStream(as.Video, as.Format)
		if err != nil {
			setErr(err)
			return
		}
		defer f.Close()

		cr := newCountingReader(f)

		if s != as.size {
			setErr(errors.New("unexpected stream size detected on open"))
			return
		}

		// TODO: instead use a truly temporary file without collision possibilities
		tmpFilePath := as.dstFilePath + ".tmp"

		escape := func(s string) string {
			return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
		}

		cmd := exec.CommandContext(ctx, "nice", "bash", "-c", "set -eo pipefail && ffmpeg -f mp4 -y -loglevel quiet -i pipe: -ar 48000 -ac 1 -vn -f s16le pipe:1 | /workspace/build/bin/convert-to-discord-opus | tee "+escape(tmpFilePath))
		cmd.Stdin = cr
		cmd.Stdout = bw

		if err := cmd.Run(); err != nil {
			setErr(fmt.Errorf("stream conversion process failed: %w", err))
			return
		}

		if cr.bytesRead != as.size {
			setErr(errors.New("unexpected end of stream"))
			return
		}

		if err := bw.Flush(); err != nil && !errors.Is(err, io.EOF) {
			setErr(fmt.Errorf("failed to flush file conversion stream: %w", err))
			return
		}

		if err := os.Rename(tmpFilePath, as.dstFilePath); err != nil {
			log.Err(err).
				Str("src", tmpFilePath).
				Str("dst", as.dstFilePath).
				Msg("failed to rename file")
			return
		}
	}()

	result.read = func(b []byte) (int, error) {
		if err := getErr(); err != nil {
			return 0, err
		}

		i, err := pr.Read(b)
		if errors.Is(err, io.EOF) {
			// wait for the temp tee file to get renamed
			<-successWaitCtx.Done()
		}

		return i, err
	}

	return &result, nil
}

var audioStreamHttpClient = http.Client{}

func playAfterTranscode(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, args map[string]string) error {
	urlStr := args["url"]

	if urlStr == "" {
		return nil
	}

	// ensure that the bot is first in a voice channel
	{
		c, err := findVoiceChannel(s, m, p)
		if err != nil {
			return fmt.Errorf("failed to auto-join a voice channel: %v", err)
		}

		if c == nil {
			return errors.New("not in a voice channel")
		}
	}

	if !p.HasAudience() {
		return errors.New("no audience in voice channel")
	}

	var dlc youtube.Client

	ytVid, err := dlc.GetVideoContext(ctx, urlStr)
	if err != nil {
		return err
	}

	cacheDir := path.Join(".media-cache", "v1", ytVid.ID)
	cachedRef := path.Join(cacheDir, "audio.discord-opus")

	if err := os.MkdirAll(cacheDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to make cache directory: %s: %v", cacheDir, err)
	}

	as := &audioStream{
		Video:       ytVid,
		httpClient:  &audioStreamHttpClient,
		dstFilePath: cachedRef,
	}

	// TODO: consider moving format selection logic into audioStream
	// if as.Cached() {
	// 	play(p, m, urlStr, as)
	// }

	formats := ytVid.Formats.Type("video/mp4")
	formats.Sort()

	for _, f := range formats {

		if f.AudioChannels <= 0 {
			continue
		}

		// TODO: in the future try to select the lowest bitrate
		// if dstFormat != nil {
		// 	if dstFormat.Bitrate <= f.Bitrate {
		// 		continue
		// 	}
		// }

		n := f

		newReader, newSize, err := dlc.GetStreamContext(ctx, ytVid, &n)
		if err != nil {
			continue
		}

		newReader.Close()

		if newSize == 0 {
			continue
		}

		// choose this stream format

		as.size = newSize
		as.Format = &n
	}

	if as == nil {
		return errors.New("failed to find a usable video format")
	}

	log.Debug().
		Str("ID", as.ID).
		Int("ItagNo", as.ItagNo).
		Int("Bitrate", as.Bitrate).
		Str("AudioQuality", as.AudioQuality).
		Str("AudioSampleRate", as.AudioSampleRate).
		Str("Quality", as.Quality).
		Str("QualityLabel", as.QualityLabel).
		Str("URL", as.URL).
		Int("AudioChannels", as.AudioChannels).
		Msg("found video format")

	play(p, m, urlStr, as)

	return nil
}

func findVoiceChannel(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) (*discordgo.VoiceConnection, error) {

	result := func() *discordgo.VoiceConnection {
		s.RLock()
		defer s.RUnlock()

		return s.VoiceConnections[m.GuildID]
	}()
	if result != nil {
		return result, nil
	}

	// find current voice channel of message sender and join it

	g, err := s.Guild(m.GuildID)
	if err != nil {
		return nil, err
	}

	for _, v := range g.VoiceStates {
		if v.UserID != m.Author.ID {
			continue
		}

		mute := false
		deaf := false

		result, err = s.ChannelVoiceJoin(m.GuildID, v.ChannelID, mute, deaf)
		if err != nil {
			return nil, err
		}

		p.SetVoiceConnection(m, v.ChannelID, result)

		return result, nil
	}

	return nil, nil
}

// play can be called multiple times
// when the cacheDir is not empty then the file will be playable
func play(p *service.Player, m *discordgo.MessageCreate, url string, ad service.AudioStreamer) {

	p.Play(m, url, m.Message.Author.ID, m.Author.Mention(), ad)
}
