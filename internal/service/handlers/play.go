package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/bwmarrin/discordgo"
	"github.com/kkdai/youtube/v2"

	"github.com/josephcopenhaver/melody-bot/internal/cache"
	"github.com/josephcopenhaver/melody-bot/internal/service"
	"github.com/josephcopenhaver/melody-bot/internal/service/server/reactions"
)

const (
	MediaCacheDir          = ".media-cache/v1"
	MediaMetadataCacheDir  = ".media-meta-cache/v1"
	MediaMetadataCacheSize = 1024 * 1024
)

type MediaMetaCacheEntry struct {
	VideoID string         `json:"video_id"`
	Format  youtube.Format `json:"format"`
	Size    int64          `json:"size"`
}

var vidMetadataCacheOptions = []cache.DiskCacheOption[string, MediaMetaCacheEntry]{
	cache.DiskCacheKeyMarshaler[string, MediaMetaCacheEntry](cache.NewKeyMarshaler(
		func(s string) ([]byte, error) {
			return []byte(s), nil
		},
	)),
	cache.DiskCacheValueTranscoder[string](cache.NewTranscoder(
		func(v MediaMetaCacheEntry) ([]byte, error) {
			var buf bytes.Buffer

			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)

			if err := enc.Encode(v); err != nil {
				return nil, err
			}

			return buf.Bytes(), nil
		},
		func(b []byte) (MediaMetaCacheEntry, error) {
			var result, buf MediaMetaCacheEntry

			if err := json.Unmarshal(b, &buf); err != nil {
				return result, err
			}

			result = buf
			return result, nil
		},
	)),
}

var vidMetadataCache *cache.DiskCache[string, MediaMetaCacheEntry]

func init() {
	if v, err := cache.NewDiskCache(MediaMetadataCacheDir, MediaMetadataCacheSize, vidMetadataCacheOptions...); err != nil {
		panic(err)
	} else {
		vidMetadataCache = v
	}
}

// TODO: handle voice channel reconnects forced by the server, specifically when forced into a channel where no one is present

func Play() HandleMessageCreate {

	return newHandleMessageCreate(
		"play",
		"play <url>",
		"append track from youtube url to the playlist",
		newRegexMatcher(
			true,
			regexp.MustCompile(`^\s*play\s+(?P<url>[^\s]+.*?)\s*$`),
			handlePlayRequest,
		),
	)
}

type audioStream struct {
	pid              service.PlaylistID
	pslc             time.Time // player state last changed
	srcVideoUrlStr   string
	size             int64
	ytApiClient      *youtube.Client
	ytDownloadClient *youtube.Client
	*youtube.Video
	*youtube.Format
	dstFilePath string
}

type flushedState struct {
	rwm     sync.RWMutex
	flushed bool
}

func (fs *flushedState) setFlushed() {
	fs.rwm.Lock()
	defer fs.rwm.Unlock()

	fs.flushed = true
}

func (fs *flushedState) Flushed() bool {
	fs.rwm.RLock()
	defer fs.rwm.RUnlock()

	return fs.flushed
}

type readCloser struct {
	flushedState
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

func (as *audioStream) PlaylistID() string {
	return as.pid.String()
}

func (as *audioStream) PlayerStateLastChangedAt() time.Time {
	return as.pslc
}

func (as *audioStream) SrcUrlStr() string {
	return as.srcVideoUrlStr
}

func (as *audioStream) SelectDownloadURL(ctx context.Context) error {

	// protect against getting called more than once
	if as.Video != nil {
		return nil
	}

	var ytVid *youtube.Video
	cacheV, ok, err := vidMetadataCache.Get(as.srcVideoUrlStr)
	if err != nil {
		return err
	}

	if ok {
		ytVid = &youtube.Video{ID: cacheV.VideoID}
		as.size = cacheV.Size
		fmt := cacheV.Format
		as.Format = &fmt
	} else {

		ytVid, err = as.ytApiClient.GetVideoContext(ctx, as.srcVideoUrlStr)
		if err != nil {
			log.Err(err).Msg("failed to get video context")
			return err
		}

		formats := ytVid.Formats.Type("video/mp4")
		formats.Sort()

		ytVid = &youtube.Video{ID: ytVid.ID}

		for i := len(formats) - 1; i >= 0; i-- {
			f := formats[i]

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

			newReader, newSize, err := as.ytApiClient.GetStreamContext(ctx, ytVid, &n)
			if err != nil {
				continue
			}
			if newReader != nil {
				newReader.Close()
			}
			if newSize == 0 {
				// try again, there is a bug in the client implementation
				newReader, newSize, err = as.ytApiClient.GetStreamContext(ctx, ytVid, &n)
				if err != nil {
					continue
				}
				if newReader != nil {
					newReader.Close()
				}
				if newSize == 0 {
					log.Error().
						Str("video_url", as.srcVideoUrlStr).
						Str("format_url", f.URL).
						Msg("stream size consistently returned zero bytes which should be impossible")
					continue
				}
			}

			// choose this stream format

			as.size = newSize
			as.Format = &n

			break
		}

		if as.Format == nil {
			log.Error().Int("search_count", len(formats)).Msg("failed to find a usable video format")
			return fmt.Errorf("failed to find a usable video format, searched %d", len(formats))
		}
	}

	cacheDir := path.Join(MediaCacheDir, ytVid.ID)
	cachedRef := path.Join(cacheDir, "audio.s16le")

	// write new values to internal state
	as.dstFilePath = cachedRef
	as.Video = ytVid

	if err := vidMetadataCache.Set(as.srcVideoUrlStr, MediaMetaCacheEntry{
		VideoID: ytVid.ID,
		Format:  *as.Format,
		Size:    as.size,
	}); err != nil {
		log.Error().
			Err(err).
			Str("key", as.srcVideoUrlStr).
			Interface("VideoFormat", *as.Format).
			Msg("failed to save a video metadata cache entry")
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

	return nil
}

func (as *audioStream) getStream(ctx context.Context, video *youtube.Video, format *youtube.Format) (io.ReadCloser, int64, error) {
	// GetStreamContext has a bug, sometimes returns zero size and no err
	//
	// TODO: open an issue/fix with the lib maintainer
	return as.ytDownloadClient.GetStreamContext(ctx, video, format)
}

func (as *audioStream) Cached() bool {

	info, err := os.Stat(as.dstFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Err(err).
				Msg("failed to stat file system")
			return false
		}

		return false
	}

	if info.Size() == 0 {
		os.Remove(as.dstFilePath)
		return false
	}

	return true
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func (as *audioStream) ReadCloser(ctx context.Context, wg *sync.WaitGroup) (io.ReadCloser, error) {

	if as.Cached() {
		log.Debug().
			Str("url", as.srcVideoUrlStr).
			Str("cached_file", as.dstFilePath).
			Msg("playing from cache")
		return os.Open(as.dstFilePath)
	}

	log.Debug().
		Str("url", as.srcVideoUrlStr).
		Msg("transcoding just in time and playing from transcode activity")

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

		log.Err(err).
			Str("src_url", as.srcVideoUrlStr).
			Str("dst_path", as.dstFilePath).
			Int64("size", as.size).
			Msg("error in audio stream read-closer")

		errResp.err = err
	}

	getErr := func() error {
		errResp.RLock()
		defer errResp.RUnlock()

		return errResp.err
	}

	cacheDir := path.Dir(as.dstFilePath)

	if err := os.MkdirAll(cacheDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to make cache directory: %s: %v", cacheDir, err)
	}

	tmpF, err := ioutil.TempFile(cacheDir, "melody-bot.*.audio.s16le.tmp")
	if err != nil {
		return nil, err
	}

	tmpFilePath := tmpF.Name()

	ignoredErr := tmpF.Close()
	_ = ignoredErr

	tmpFilePathCleanup := func() {
		os.Remove(tmpFilePath)
	}

	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(ctx)
	result := readCloser{
		close: func() error {
			defer cancel()
			defer func() {
				if tmpFilePathCleanup == nil {
					return
				}

				log.Debug().
					Err(getErr()).
					Str("src_url", as.srcVideoUrlStr).
					Str("dst_path", as.dstFilePath).
					Msg("could not cache audio stream")

				tmpFilePathCleanup()
			}()
			pr.Close()
			return nil
		},
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()

		defer pw.Close()

		log.Debug().
			Int64("content-length", as.Format.ContentLength).
			Str("video-url", as.srcVideoUrlStr).
			Msg("getting stream")

		f, s, err := as.getStream(ctx, as.Video, as.Format)
		if err != nil {
			setErr(err)
			return
		}
		defer func() {
			if f != nil {
				f.Close()
			}
		}()
		if s == 0 {
			if v := f; v != nil {
				f = nil
				v.Close()
			}

			as.Video = nil
			as.size = 0
			as.Format = nil

			if err := vidMetadataCache.Delete(as.srcVideoUrlStr); err != nil {
				setErr(err)
				return
			}

			if err := as.SelectDownloadURL(ctx); err != nil {
				setErr(err)
				return
			}
			f, s, err = as.getStream(ctx, as.Video, as.Format)
			if err != nil {
				setErr(err)
				return
			}
			if s == 0 {
				setErr(errors.New("failed to get stream context"))
				return
			}
		}

		cr := newCountingReader(f)

		if s != as.size {
			setErr(fmt.Errorf("unexpected stream size detected on open, expected %d, got %d", as.size, s))
			return
		}

		cmd := exec.CommandContext(ctx, "bash", "-c", "set -eo pipefail && ffmpeg -f mp4 -y -loglevel quiet -i pipe: -ar "+strconv.Itoa(service.SampleRate)+" -ac 1 -vn -f s16le pipe:1 | tee "+shellEscape(tmpFilePath))
		cmd.Stdin = cr
		bw := bufio.NewWriter(pw)
		cmd.Stdout = bw
		defer bw.Flush()

		if err := cmd.Run(); err != nil {
			setErr(fmt.Errorf("stream conversion process failed: %s", err.Error()))
			return
		}

		if cr.bytesRead != as.size {
			err := errors.New("unexpected end of stream during transcode+reader")

			log.Err(err).
				Str("src_url", as.srcVideoUrlStr).
				Str("dst_path", as.dstFilePath).
				Int64("bytes_read", cr.bytesRead).
				Int64("size", as.size).
				Msg("failed to download all bytes from source stream for transcode+reader")

			setErr(err)
			return
		}

		result.setFlushed()
	}()

	result.read = func(b []byte) (int, error) {
		if err := getErr(); err != nil {
			return 0, err
		}

		i, err := pr.Read(b)
		if err != nil {
			if errors.Is(err, io.EOF) {

				// fully wait for writer to return
				<-ctx.Done()

				// double check there was no error in the transcode activity
				if err := getErr(); err != nil {
					return 0, err
				}

				if err := os.Rename(tmpFilePath, as.dstFilePath); err != nil {
					log.Err(err).
						Str("src", tmpFilePath).
						Str("dst", as.dstFilePath).
						Msg("failed to rename file")

					return i, err
				}

				// tmp file is now gone, don't try to remove it
				tmpFilePathCleanup = nil

				log.Debug().
					Str("src_url", as.srcVideoUrlStr).
					Str("dst_path", as.dstFilePath).
					Msg("cached audio stream")

				return i, err
			} else {
				cancel()
			}

			return i, err
		}

		return i, nil
	}

	return &result, nil
}

// DownloadAndTranscode synchronously downloads and transcodes the audio stream to disk
//
// The audio stream should be considered closed after a call is made to this function
// and it cannot be mixed with the async ReadCloser func.
func (as *audioStream) DownloadAndTranscode(ctx context.Context) error {

	log.Debug().
		Str("url", as.srcVideoUrlStr).
		Msg("downloading and transcoding to cache")

	cacheDir := path.Dir(as.dstFilePath)

	if err := os.MkdirAll(cacheDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to make cache directory: %s: %v", cacheDir, err)
	}

	tmpF, err := ioutil.TempFile(cacheDir, "melody-bot.*.audio.s16le.tmp")
	if err != nil {
		return err
	}

	tmpFilePath := tmpF.Name()

	ignoredErr := tmpF.Close()
	_ = ignoredErr

	cleanup := func() {
		os.Remove(tmpFilePath)
	}
	defer func() {
		if f := cleanup; f != nil {
			cleanup = nil
			f()
		}
	}()

	log.Debug().
		Int64("content-length", as.Format.ContentLength).
		Str("video-url", as.srcVideoUrlStr).
		Msg("getting stream")

	f, s, err := as.getStream(ctx, as.Video, as.Format)
	if err != nil {
		return err
	}
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	if s == 0 {
		if v := f; v != nil {
			f = nil
			v.Close()
		}

		as.Video = nil
		as.size = 0
		as.Format = nil

		if err := vidMetadataCache.Delete(as.srcVideoUrlStr); err != nil {
			return err
		}

		if err := as.SelectDownloadURL(ctx); err != nil {
			return err
		}
		f, s, err = as.getStream(ctx, as.Video, as.Format)
		if err != nil {
			return err
		}
		if s == 0 {
			return errors.New("failed to get stream context")
		}
	}

	cr := newCountingReader(f)

	if s != as.size {
		return fmt.Errorf("unexpected stream size detected on open, expected %d, got %d", as.size, s)
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", "set -eo pipefail && ffmpeg -f mp4 -y -loglevel quiet -i pipe: -ar "+strconv.Itoa(service.SampleRate)+" -ac 1 -vn -f s16le "+shellEscape(tmpFilePath))
	cmd.Stdin = cr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stream conversion process failed: %s", err.Error())
	}

	if cr.bytesRead != as.size {
		err := errors.New("unexpected end of stream during DownloadAndTranscode")

		log.Err(err).
			Str("src_url", as.srcVideoUrlStr).
			Str("dst_path", as.dstFilePath).
			Int64("bytes_read", cr.bytesRead).
			Int64("size", as.size).
			Msg("failed to download all bytes from source stream for transcode+cache")

		return err
	}

	if err := os.Rename(tmpFilePath, as.dstFilePath); err != nil {
		log.Err(err).
			Str("src", tmpFilePath).
			Str("dst", as.dstFilePath).
			Msg("failed to rename file")

		return err
	}

	cleanup = nil

	return nil
}

var apiHttpClient = http.Client{
	Timeout: 10 * time.Second,
}

var audioStreamHttpClient = http.Client{
	Timeout: 17 * time.Hour,
}

func newYoutubeApiClient() *youtube.Client {
	return &youtube.Client{
		HTTPClient: &apiHttpClient,
	}
}

func newYoutubeDownloadClient() *youtube.Client {
	return &youtube.Client{
		HTTPClient: &audioStreamHttpClient,
	}
}

func handlePlayRequest(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, args map[string]string) error {
	pid := p.PlaylistID()
	pslc := p.StateLastChangedAt()

	urlStr := args["url"]

	playPack := make(chan service.PlayCall, 1)

	p.Enqueue(playPack)

	var play func(ctx context.Context, as *audioStream)
	{
		mention := m.Author.Mention()
		play = func(ctx context.Context, as *audioStream) {
			as.pid = pid
			as.pslc = pslc
			pc := service.PlayCall{
				MessageCreate: m,
				AuthorID:      m.Message.Author.ID,
				AuthorMention: mention,
				AudioStreamer: as,
			}

			ctxDone := ctx.Done()
			select {
			case <-ctxDone:
				return
			default:
			}
			select {
			case <-ctxDone:
				return
			case playPack <- pc:
			}
		}
	}

	var closePlayPack func()
	{
		var oncer sync.Once
		closePlayPack = func() {
			oncer.Do(func() {
				close(playPack)
			})
		}
	}

	// ensure the channel is absolutely always closed even if something panics
	defer func() {
		if r := recover(); r != nil {
			defer panic(r)

			defer closePlayPack()

			log.Ctx(ctx).Error().
				Err(errors.New("panicking")).
				Msg("this should never happen: open a ticket")
		}
	}()

	if handled, err := processPlaylist(ctx, s, m, p, play, closePlayPack, urlStr); err != nil {
		closePlayPack()
		return err
	} else if handled {
		// handling async, don't close the play package
		return nil
	}

	defer closePlayPack()

	return playAfterTranscode(ctx, s, m, p, play, urlStr)
}

var ErrPanicInPlaylistLoader = errors.New("Panic in playlist loader")

func processPlaylist(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, play func(context.Context, *audioStream), closePlayPack func(), urlStr string) (bool, error) {
	var result bool

	ac := newYoutubeApiClient()

	var u *url.URL
	if v, err := url.Parse(urlStr); err != nil || v == nil {
		log.Ctx(ctx).Debug().
			Err(err).
			Str("url", urlStr).
			Msg("failed to parse url argument")
		return result, nil
	} else {
		u = v
	}

	if u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}

	if u.Path != "/playlist" && u.Path != "/playlist/" {
		log.Ctx(ctx).Debug().
			Str("url", urlStr).
			Msg("not a playlist url")
		return result, nil
	}

	// take ownership of the request handling context:
	result = true

	var extCancel *func(error)
	var cancel func(error)
	var wg sync.WaitGroup
	{
		var cancelCauseFunc context.CancelCauseFunc
		ctx, cancelCauseFunc = context.WithCancelCause(ctx)
		var oncer sync.Once
		cancel = func(err error) {
			oncer.Do(func() {
				p.DeregisterCanceler(extCancel)

				if cerr := ctx.Err(); cerr != nil {
					err = cerr
				}

				cancelCauseFunc(err)
			})
		}
		f := func(err error) {
			defer wg.Wait()

			cancel(err)
		}
		extCancel = &f
	}

	wg.Add(1)
	p.RegisterCanceler(extCancel)
	go func() {
		defer wg.Done()
		defer closePlayPack()

		var err error
		defer func() {
			if err == nil {
				if r := recover(); r != nil {
					defer panic(r)

					if v, ok := r.(error); ok {
						err = v
					} else {
						err = ErrPanicInPlaylistLoader
					}
				}
			}

			cancel(err)
		}()
		err = func() error {
			if err := ctx.Err(); err != nil {
				return err
			}

			pl, err := ac.GetPlaylistContext(ctx, urlStr)
			if err != nil {
				return fmt.Errorf("failed to download playlist: %w", err)
			}

			// TODO: cache playlist video urls for playlist url string

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

			if len(pl.Videos) == 0 {
				return errors.New("youtube playlist was empty")
			}

			var numFailed, numSuccess int
			for _, v := range pl.Videos {
				if err := ctx.Err(); err != nil {
					return err
				}

				as := &audioStream{
					srcVideoUrlStr:   fmt.Sprintf("https://www.youtube.com/watch?v=%s", url.QueryEscape(v.ID)),
					ytApiClient:      ac,
					ytDownloadClient: newYoutubeDownloadClient(),
				}

				if err := as.SelectDownloadURL(ctx); err != nil {
					log.Ctx(ctx).Err(err).
						Str("track", as.srcVideoUrlStr).
						Msg("failed to select download url")

					p.BroadcastTextMessage("Failed to queue " + as.srcVideoUrlStr)

					numFailed += 1
					continue
				}

				numSuccess += 1

				if err := ctx.Err(); err != nil {
					return err
				}

				play(ctx, as)
			}

			if numFailed > 0 {
				if numSuccess == 0 {
					return fmt.Errorf("all %d tracks could not be imported from playlist", numFailed)
				}
				return reactions.NewWarning(fmt.Errorf("%d out of %d tracks could not be imported from playlist", numFailed, numFailed+numSuccess))
			}

			return nil
		}()
	}()

	return result, nil
}

func playAfterTranscode(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, play func(context.Context, *audioStream), urlStr string) error {

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

	as := &audioStream{
		srcVideoUrlStr:   urlStr,
		ytApiClient:      newYoutubeApiClient(),
		ytDownloadClient: newYoutubeDownloadClient(),
	}

	if err := as.SelectDownloadURL(ctx); err != nil {
		return err
	}

	play(ctx, as)

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

	g, err := s.State.Guild(m.GuildID)
	if err != nil {
		return result, err
	}

	// find current voice channel of message sender and join it

	var chanID string
	for _, v := range g.VoiceStates {
		if v.UserID != m.Author.ID {
			continue
		}

		chanID = v.ChannelID
		break
	}

	if chanID == "" {
		return nil, nil
	}

	mute := false
	deaf := true

	err = nil
	for tryCount := 0; tryCount < 6; tryCount++ {
		result, err = s.ChannelVoiceJoin(m.GuildID, chanID, mute, deaf) // can take up to 10 seconds to return a timeout error
		if err == nil {
			break
		}

		if v := result; v != nil {
			result = nil

			// The ChannelVoiceJoin call calls Close, but not Disconnect, it leaves the client in a connected state with an unready - but logically closed connection
			// TODO: open bug with library maintainer
			func() {
				s.Lock()
				defer s.Unlock()

				delete(s.VoiceConnections, m.GuildID)
			}()
		}

		time.Sleep(1 * time.Second) // TODO: find a better way to deal with: error: failed to auto-join a voice channel: timeout waiting for voice
	}

	p.SetVoiceConnection(m, chanID, result)

	return result, nil
}
