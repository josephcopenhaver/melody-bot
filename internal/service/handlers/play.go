package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/cache"
	"github.com/josephcopenhaver/melody-bot/internal/logging"
	"github.com/josephcopenhaver/melody-bot/internal/service"
	"github.com/josephcopenhaver/melody-bot/internal/service/server/reactions"
	"github.com/kkdai/youtube/v2"
)

const (
	MediaCacheDir          = ".media-cache/v1"
	MediaMetadataCacheDir  = ".media-meta-cache/v1"
	MediaMetadataCacheSize = 1024 * 1024
)

var (
	ErrVoiceChannelNotFound = errors.New("could not find voice channel")
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

//nolint:gochecknoinits
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

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
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
	return as.SelectDownloadURLWithFallbackApiClient(ctx, nil)
}

// SelectDownloadURLWithFallbackApiClient is a jenky test that can modify the state of the audioStream apiClient if newApiClient is non-nil
func (as *audioStream) SelectDownloadURLWithFallbackApiClient(ctx context.Context, newApiClient func() *youtube.Client) error {

	// protect against getting called more than once
	if as.Video != nil {
		return nil
	}

	var ytVid *youtube.Video
	cacheV, cacheHit, err := vidMetadataCache.Get(as.srcVideoUrlStr)
	if err != nil {
		return err
	}

	if cacheHit {

		ytVid = &youtube.Video{ID: cacheV.VideoID}
		as.size = cacheV.Size
		fmt := cacheV.Format
		as.Format = &fmt
	} else {

		ytVid, err = as.ytApiClient.GetVideoContext(ctx, as.srcVideoUrlStr)
		if err != nil {
			logging.Context(ctx).ErrorContext(ctx,
				"failed to get video context",
				"error", err,
			)
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
				if f := newApiClient; f != nil {
					if v := f(); v != nil {
						as.ytApiClient = v
					}
				}
				// try again, there is a bug in the client implementation
				newReader, newSize, err = as.ytApiClient.GetStreamContext(ctx, ytVid, &n)
				if err != nil {
					continue
				}
				if newReader != nil {
					newReader.Close()
				}
				if newSize == 0 {
					logging.Context(ctx).ErrorContext(ctx,
						"stream size consistently returned zero bytes which should be impossible",
						"video_url", as.srcVideoUrlStr,
						"format_url", f.URL,
					)
					continue
				}
			}

			// choose this stream format

			as.size = newSize
			as.Format = &n

			break
		}

		if as.Format == nil {
			logging.Context(ctx).ErrorContext(ctx,
				"failed to find a usable video format",
				"search_count", len(formats),
			)
			return fmt.Errorf("failed to find a usable video format, searched %d", len(formats))
		}
	}

	cacheDir := path.Join(MediaCacheDir, ytVid.ID)
	cachedRef := path.Join(cacheDir, "audio.s16le")

	// write new values to internal state
	as.dstFilePath = cachedRef
	as.Video = ytVid

	if !cacheHit {
		cacheV = MediaMetaCacheEntry{
			VideoID: ytVid.ID,
			Format:  *as.Format,
			Size:    as.size,
		}

		if err := vidMetadataCache.Set(as.srcVideoUrlStr, cacheV); err != nil {
			logging.Context(ctx).ErrorContext(ctx,
				"failed to save a video metadata cache entry",
				"error", err,
				"key", as.srcVideoUrlStr,
				"VideoFormat", *as.Format,
			)
		}
	}

	logging.Context(ctx).DebugContext(ctx,
		"found video format",
		"ID", as.ID,
		"ItagNo", as.ItagNo,
		"Bitrate", as.Bitrate,
		"AudioQuality", as.AudioQuality,
		"AudioSampleRate", as.AudioSampleRate,
		"Quality", as.Quality,
		"QualityLabel", as.QualityLabel,
		"URL", as.URL,
		"AudioChannels", as.AudioChannels,
	)

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
			slog.Error(
				"failed to stat file system",
				"error", err,
			)
			return false
		}

		return false
	}

	if info.Size() == 0 {
		if err := os.Remove(as.dstFilePath); err != nil {
			slog.Error(
				"failed to remove empty file",
				"error", err,
			)
		}
		return false
	}

	return true
}

//nolint:gocyclo
func (as *audioStream) ReadCloser(ctx context.Context, wg *sync.WaitGroup) (io.ReadCloser, error) {

	if as.Cached() {
		logging.Context(ctx).DebugContext(ctx,
			"playing from cache",
			"url", as.srcVideoUrlStr,
			"cached_file", as.dstFilePath,
		)
		return os.Open(as.dstFilePath)
	}

	logging.Context(ctx).DebugContext(ctx,
		"transcoding just in time and playing from transcode activity",
		"url", as.srcVideoUrlStr,
	)

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

		logging.Context(ctx).ErrorContext(ctx,
			"error in audio stream read-closer",
			"error", err,
			"src_url", as.srcVideoUrlStr,
			"dst_path", as.dstFilePath,
			"size", as.size,
		)

		errResp.err = err
	}

	getErr := func() error {
		errResp.RLock()
		defer errResp.RUnlock()

		return errResp.err
	}

	cacheDir := path.Dir(as.dstFilePath)

	if err := os.MkdirAll(cacheDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to make cache directory: %s: %w", cacheDir, err)
	}

	tmpF, err := os.CreateTemp(cacheDir, "melody-bot.*.audio.s16le.tmp")
	if err != nil {
		return nil, err
	}

	tmpFilePath := tmpF.Name()

	ignoredErr := tmpF.Close()
	_ = ignoredErr

	var teeDst io.WriteCloser
	var teeOK bool
	var fileCreateTried bool

	tmpFilePathCleanup := func() {
		defer os.Remove(tmpFilePath)

		if f := teeDst; f != nil {
			teeDst = nil
			f.Close()
		}
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

				logger := slog.Default()
				if err := getErr(); err != nil {
					logger = logger.With("error", err)
				}

				logger.Debug(
					"could not cache audio stream",
					"src_url", as.srcVideoUrlStr,
					"dst_path", as.dstFilePath,
				)

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

		slog.Debug(
			"getting stream",
			"content-length", as.Format.ContentLength,
			"video-url", as.srcVideoUrlStr,
		)

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

		cmd := exec.CommandContext(ctx, "ffmpeg", "-f", "mp4", "-y", "-loglevel", "quiet", "-i", "pipe:", "-ar", strconv.Itoa(service.SampleRate), "-ac", "1", "-vn", "-f", "s16le", "pipe:1")
		cmd.Stdin = cr
		bw := bufio.NewWriter(pw)
		cmd.Stdout = bw
		defer bw.Flush()

		if err := cmd.Run(); err != nil {
			setErr(fmt.Errorf("stream conversion process failed: %w", err))
			return
		}

		if cr.bytesRead != as.size {
			err := errors.New("unexpected end of stream during transcode+reader")

			slog.Error(
				"failed to download all bytes from source stream for transcode+reader",
				"error", err,
				"src_url", as.srcVideoUrlStr,
				"dst_path", as.dstFilePath,
				"bytes_read", cr.bytesRead,
				"size", as.size,
			)

			setErr(err)
			return
		}

		result.setFlushed()
	}()

	result.read = func(b []byte) (int, error) {
		if err := getErr(); err != nil {
			return 0, err
		}

		if teeDst == nil && !fileCreateTried {
			fileCreateTried = true

			if f, err := os.Create(tmpFilePath); err != nil {
				slog.Error(
					"failed to open file for writing",
					"error", err,
					"file", tmpFilePath,
				)
			} else {
				teeDst = f
				teeOK = true
			}
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

				// handle tee file state
				if renameErr := func() error {
					if !teeOK {
						return nil
					}

					if i > 0 {
						if _, err := teeDst.Write(b[:i]); err != nil {
							slog.Error(
								"failed to write to tee tmp file near end of transcode",
								"error", err,
							)
							teeOK = false
							return nil
						}
					}

					f := teeDst
					teeDst = nil  // don't let defer try to close it again
					teeOK = false // don't attempt to write to tee again
					if err := f.Close(); err != nil {
						slog.Error(
							"failed to close tee tmp file near end of transcode",
							"error", err,
						)
						return nil
					}

					if err := os.Rename(tmpFilePath, as.dstFilePath); err != nil {
						slog.Error(
							"failed to rename file",
							"error", err,
							"src", tmpFilePath,
							"dst", as.dstFilePath,
						)
						return err
					}

					// tmp file is now gone, don't try to remove it
					tmpFilePathCleanup = nil

					slog.Debug(
						"cached audio stream",
						"src_url", as.srcVideoUrlStr,
						"dst_path", as.dstFilePath,
					)

					return nil
				}(); renameErr != nil {
					return i, renameErr
				}
			}

			cancel()

			return i, err
		}

		if teeOK && i > 0 {
			if _, err := teeDst.Write(b[:i]); err != nil {
				teeOK = false
				slog.Error(
					"failed to append to tee tmp file",
					"error", err,
				)
			}
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

	slog.Debug(
		"downloading and transcoding to cache",
		"url", as.srcVideoUrlStr,
	)

	cacheDir := path.Dir(as.dstFilePath)

	if err := os.MkdirAll(cacheDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to make cache directory: %s: %w", cacheDir, err)
	}

	tmpF, err := os.CreateTemp(cacheDir, "melody-bot.*.audio.s16le.tmp")
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

	slog.Debug(
		"getting stream",
		"content-length", as.Format.ContentLength,
		"video-url", as.srcVideoUrlStr,
	)

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

	cmd := exec.CommandContext(ctx, "ffmpeg", "-f", "mp4", "-y", "-loglevel", "quiet", "-i", "pipe:", "-ar", strconv.Itoa(service.SampleRate), "-ac", "1", "-vn", "-f", "s16le", tmpFilePath)
	cmd.Stdin = cr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stream conversion process failed: %w", err)
	}

	if cr.bytesRead != as.size {
		err := errors.New("unexpected end of stream during DownloadAndTranscode")

		slog.Error(
			"failed to download all bytes from source stream for transcode+cache",
			"error", err,
			"src_url", as.srcVideoUrlStr,
			"dst_path", as.dstFilePath,
			"bytes_read", cr.bytesRead,
			"size", as.size,
		)

		return err
	}

	if err := os.Rename(tmpFilePath, as.dstFilePath); err != nil {
		slog.Error(
			"failed to rename file",
			"src", tmpFilePath,
			"dst", as.dstFilePath,
		)

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

			logging.Context(ctx).ErrorContext(ctx,
				"this should never happen",
				"error", errors.New("panicking"),
				"recommendation", "open a ticket",
			)
		}
	}()

	if processPlaylist(ctx, s, m, p, play, closePlayPack, urlStr) {
		// handling async, don't close the play package
		return nil
	}

	defer closePlayPack()

	return playAfterTranscode(ctx, s, m, p, play, urlStr)
}

var ErrPanicInPlaylistLoader = errors.New("panic in playlist loader")

//nolint:gocyclo
func processPlaylist(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, play func(context.Context, *audioStream), closePlayPack func(), urlStr string) bool {
	var result bool

	ac := newYoutubeApiClient()

	var u *url.URL
	{
		v, err := url.Parse(urlStr)
		if err != nil || v == nil {
			logging.Context(ctx).DebugContext(ctx,
				"failed to parse url argument",
				"url", urlStr,
			)
			return result
		}

		u = v
	}

	if u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}

	if u.Path != "/playlist" && u.Path != "/playlist/" {
		logging.Context(ctx).DebugContext(ctx,
			"not a playlist url",
			"url", urlStr,
		)
		return result
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
				_, err := findVoiceChannel(s, m, p)
				if err != nil {
					return fmt.Errorf("failed to auto-join a voice channel: %w", err)
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
					logging.Context(ctx).ErrorContext(ctx,
						"failed to select download url",
						"error", err,
						"track", as.srcVideoUrlStr,
					)

					p.BroadcastTextMessage("Failed to queue " + as.srcVideoUrlStr)

					numFailed++
					continue
				}

				numSuccess++

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

	return result
}

func playAfterTranscode(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, play func(context.Context, *audioStream), urlStr string) error {

	if urlStr == "" {
		return nil
	}

	// ensure that the bot is first in a voice channel
	{
		_, err := findVoiceChannel(s, m, p)
		if err != nil {
			return fmt.Errorf("failed to auto-join a voice channel: %w", err)
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

//nolint:unparam
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
		return nil, ErrVoiceChannelNotFound
	}

	mute := false
	deaf := true

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
