package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
	"github.com/josephcopenhaver/melody-bot/internal/service/server/reactions"
	"github.com/rs/zerolog/log"
)

type serialTaskRunner struct {
	rwm       sync.RWMutex
	startedAt time.Time
	wg        sync.WaitGroup
	taskChan  chan func(context.Context)
}

func newSerialTaskRunner(size int) *serialTaskRunner {
	return &serialTaskRunner{
		taskChan: make(chan func(context.Context), size),
	}
}

func (tr *serialTaskRunner) Enqueue(f func(context.Context)) {
	tr.taskChan <- f
}

func (tr *serialTaskRunner) Wait() {
	tr.wg.Wait()
}

func (tr *serialTaskRunner) Start(ctx context.Context) {
	tr.rwm.RLock()
	cleanup := tr.rwm.RUnlock
	defer func() {
		if f := cleanup; f != nil {
			cleanup = nil
			f()
		}
	}()

	if !tr.startedAt.IsZero() {
		return
	}

	if f := cleanup; f != nil {
		cleanup = nil
		f()
	}

	tr.rwm.Lock()
	cleanup = tr.rwm.Unlock

	if !tr.startedAt.IsZero() {
		return
	}

	wg := &tr.wg

	taskChan := tr.taskChan

	ctxDone := ctx.Done()
	wg.Add(1)
	tr.startedAt = time.Now()
	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctxDone:
				return
			default:
			}
			select {
			case <-ctxDone:
				return
			case f := <-taskChan:
				func() {
					defer func() {
						if r := recover(); r != nil {
							err, ok := r.(error)
							if !ok {
								err = errors.New("cause unknown")
							}
							log.Err(err).
								Msg("panic in serial downloader")
						}
					}()

					f(ctx)
				}()
			}
		}
	}()
}

var serialDownloader *serialTaskRunner

func init() {
	serialDownloader = newSerialTaskRunner(128)
}

func SerialDownloader() *serialTaskRunner {
	return serialDownloader
}

func Cache() HandleMessageCreate {

	return newHandleMessageCreate(
		"cache-url",
		"cache <url>",
		"process music from a video url for playing at a future time",
		newRegexMatcher(
			true,
			regexp.MustCompile(`^\s*cache\s+(?P<url>[^\s]+.*?)\s*$`),
			func(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, args map[string]string) error {

				u, err := url.Parse(args["url"])
				if err != nil {
					return err
				}

				if u.Path == "/playlist" || u.Path == "/playlist/" {
					return downloadPlaylistAudioStreamsAsync(ctx, p, u.String())
				}

				return downloadAudioStreamAsync(ctx, p, u.String())
			},
		),
	)
}

var ErrPanicInCacher = errors.New("Panic in cacher")

func downloadAudioStreamAsync(ctx context.Context, p *service.Player, urlStr string) error {

	as := &audioStream{
		srcVideoUrlStr:   urlStr,
		ytApiClient:      newYoutubeApiClient(),
		ytDownloadClient: newYoutubeDownloadClient(),
	}

	SerialDownloader().
		Enqueue(asyncDownloadFunc(p, as))

	return nil
}

func downloadPlaylistAudioStreamsAsync(ctx context.Context, p *service.Player, urlStr string) error {

	sd := SerialDownloader()

	ac := newYoutubeApiClient()

	if err := ctx.Err(); err != nil {
		return err
	}

	pl, err := ac.GetPlaylistContext(ctx, urlStr)
	if err != nil {
		return fmt.Errorf("failed to download playlist: %w", err)
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

		sd.Enqueue(asyncDownloadFunc(p, as))
	}

	if numFailed > 0 {
		if numSuccess == 0 {
			return fmt.Errorf("all %d tracks could not be imported from playlist", numFailed)
		}
		return reactions.NewWarning(fmt.Errorf("%d out of %d tracks could not be imported from playlist", numFailed, numFailed+numSuccess))
	}

	return nil
}

func asyncDownloadFunc(p *service.Player, as *audioStream) func(context.Context) {
	return func(ctx context.Context) {
		var err error
		defer func() {
			if err == nil {
				if r := recover(); r != nil {
					defer panic(r)

					if v, ok := r.(error); ok {
						err = v
					} else {
						err = ErrPanicInCacher
					}
				} else {
					return
				}
			}

			p.BroadcastTextMessage(err.Error())
		}()

		// establish the dstFilePath via the download url
		if e := as.SelectDownloadURL(ctx); e != nil {
			err = e
			return
		}

		if inf, e := os.Stat(as.dstFilePath); e != nil {
			if !os.IsNotExist(e) {
				err = e
				return
			}
		} else if inf.IsDir() {
			err = errors.New("cannot download: destination file exists as a directory")
			return
		} else {
			p.BroadcastTextMessage(fmt.Sprintf("audio file for %s is already cached", as.srcVideoUrlStr))
			return
		}

		if e := as.DownloadAndTranscode(ctx); e != nil {
			err = fmt.Errorf("cache: download and transcode process for %s failed: %w", as.srcVideoUrlStr, e)
		}
	}
}
