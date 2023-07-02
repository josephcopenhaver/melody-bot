package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

func Cache() HandleMessageCreate {

	return newHandleMessageCreate(
		"cache-url",
		"cache <url>",
		"process music from a video url for playing at a later time",
		newRegexMatcher(
			true,
			regexp.MustCompile(`^\s*cache\s+(?P<url>[^\s]+.*?)\s*$`),
			func(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, args map[string]string) error {

				u, err := url.Parse(args["url"])
				if err != nil {
					return err
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

	if err := as.SelectDownloadURL(ctx); err != nil {
		return err
	}

	if inf, err := os.Stat(as.dstFilePath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else if inf.IsDir() {
		return errors.New("cannot download: destination file exists as a directory")
	} else {
		p.BroadcastTextMessage(fmt.Sprintf("audio file for %s is already cached", urlStr))
		return nil
	}

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

	// download and transcode the file

	rc, err := as.ReadCloser(ctx, &wg)
	if err != nil {
		(*extCancel)(err)
		return err
	}

	wg.Add(1)
	p.RegisterCanceler(extCancel)
	go func() {
		defer wg.Done()
		defer rc.Close()

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
				}
			} else {
				p.BroadcastTextMessage(err.Error())
			}

			cancel(err)
		}()

		var buf []byte
		{
			b := [4096]byte{}
			buf = b[:]
		}

		for {
			_, readErr := rc.Read(buf)
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					err = errors.New(fmt.Sprintf("cache: download and transcode process for %s failed: %w", urlStr, readErr))
					return
				}

				return
			}
		}
	}()

	return nil
}
