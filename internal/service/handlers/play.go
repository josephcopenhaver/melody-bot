package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/bwmarrin/discordgo"
	"github.com/rylio/ytdl"

	"github.com/josephcopenhaver/discord-bot/internal/service"
)

const (
	AudioFileName = "audio.discord-opus"
)

// TODO: handle voice channel reconnects forced by the server

// TODO: download raw video to tmp subfolder

var rePlay = regexp.MustCompile(`^\s*play\s+(?P<url>[^\s]+.*?)\s*$`)

// TODO: lock a file for prcoessing by a thread
// but automatically release the lock if the process explodes

func Play(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, handled *bool) error {

	args := regexMap(rePlay, m.Message.Content)
	if args == nil {
		return nil
	}

	urlStr := args["url"]

	if urlStr == "" {
		return nil
	}

	*handled = true

	// ensure that the bot is first in a voice channel
	c := func() *discordgo.VoiceConnection {
		s.RLock()
		defer s.RUnlock()

		return s.VoiceConnections[m.GuildID]
	}()
	if c == nil {
		return errors.New("not in a voice channel")
	}

	dlc := ytdl.Client{
		HTTPClient: http.DefaultClient,
		Logger:     log.Logger,
	}

	vidInfo, err := dlc.GetVideoInfo(context.Background(), urlStr)
	if err != nil {
		return fmt.Errorf("failed to get video info: %v", err)
	} else if vidInfo.ID == "" {
		return errors.New("failed to get video id")
	}

	// log.Debug().
	// 	Interface("video_info", vidInfo).
	// 	Msg("video info")

	cacheDir := path.Join(".media-cache", "v1", vidInfo.ID)
	downloadedRef := path.Join(cacheDir, ".dl-complete")

	err = os.MkdirAll(cacheDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to make cache directory: %s: %v", cacheDir, err)
	}

	// if already downloaded, short circuit
	_, err = os.Stat(downloadedRef)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read file system: %v", err)
		}
	} else {

		_, err = s.ChannelMessageSend(m.ChannelID, "download skipped, cached: "+urlStr)
		if err != nil {
			log.Err(err).
				Msg("failed to send play from cache confirmation")
		}

		play(p, urlStr, cacheDir)
		return nil
	}

	_, err = s.ChannelMessageSend(m.ChannelID, "downloading audio file: "+urlStr)
	if err != nil {
		log.Err(err).
			Msg("failed to send download start msg")
	}

	var dstFilePath string
	var dstFormat *ytdl.Format

	// TODO: find the lowest size video format
	for _, f := range vidInfo.Formats {

		if strings.ToLower(f.Extension) != "mp4" {
			continue
		}

		dstFilePath = path.Join(cacheDir, "video.mp4")
		dstFormat = f
		break
	}

	if dstFormat == nil {
		return errors.New("failed to find a usable video format")
	}

	log.Warn().
		Str("author_id", m.Author.ID).
		Str("author_username", m.Author.Username).
		Str("message_id", m.ID).
		Interface("message_timestamp", m.Message.Timestamp).
		Msg("video download starting")

	err = func() error {

		f, err := os.OpenFile(dstFilePath, os.O_WRONLY|os.O_CREATE, 0664)
		if err != nil {
			return err
		}

		// TODO: instead of deleting incomplete downloads, try appending to whatever previous progress has been done

		cleanup := func() {
			f.Close()
			os.Remove(dstFilePath)
		}
		defer func() {
			cleanup()
		}()

		err = dlc.Download(context.Background(), vidInfo, dstFormat, f)
		if err != nil {
			return err
		}

		cleanup = func() {}

		return f.Close()
	}()
	if err != nil {
		return fmt.Errorf("download interrupted: %v", err)
	}

	_, err = s.ChannelMessageSend(m.ChannelID, "download complete, transcode starting: "+urlStr)
	if err != nil {
		log.Err(err).
			Msg("failed to send download done msg")
	}

	err = extractAudio(dstFilePath)
	if err != nil {
		return fmt.Errorf("failed to extract audio: %v", err)
	}

	// remove no longer useful raw video file
	err = os.Remove(dstFilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cached video file: %s: %v", dstFilePath, err)
	}

	fi, err := os.OpenFile(downloadedRef, os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("failed to create download complete indicator: %v", err)
	}
	_ = fi.Close() // don't care about error here, just wanted to create the file and we did

	_, err = s.ChannelMessageSend(m.ChannelID, "transcode complete, queuing: "+urlStr)
	if err != nil {
		log.Err(err).
			Msg("failed to send download done msg")
	}

	play(p, urlStr, cacheDir)
	return nil
}

func extractAudio(vidPath string) error {

	log.Warn().
		Str("file", vidPath).
		Msg("post-processing download")

	tmpDir := path.Join(path.Dir(vidPath), "tmp")
	err := os.RemoveAll(tmpDir)
	if err != nil {
		return fmt.Errorf("failed to ensure tmp dir was removed: %s: %v", tmpDir, err)
	}

	err = os.MkdirAll(tmpDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to make tmp dir: %s: %v", tmpDir, err)
	}

	// ensure video is removed from file
	onlyAudio := path.Join(tmpDir, "only-audio."+path.Base(vidPath))
	{

		cmd := exec.Command("nice", "ffmpeg", "-y", "-loglevel", "quiet", "-i", vidPath, "-ar", strconv.Itoa(service.SampleRate), "-ac", "1", "-vn", onlyAudio)

		// TODO: capture and log instead
		cmd.Stdin = nil
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("ffmpeg command (audio only) failed: %v", err)
		}

		log.Info().Str("video_path", vidPath).Msg("done extracting audio")
	}

	// normalize audio using EBU: https://en.wikipedia.org/wiki/EBU_R_128
	normEbuWav := path.Join(tmpDir, "norm-ebu.wav")
	{

		cmd := exec.Command("nice", "ffmpeg-normalize", "--quiet", "-ar", strconv.Itoa(service.SampleRate), onlyAudio, "-o", normEbuWav)

		// TODO: capture and log instead
		cmd.Stdin = nil
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("ffmpeg-normalize command (normalize audio) failed: %v", err)
		}

		log.Info().Str("video_path", vidPath).Msg("done normalizing loudness to .wav format")
	}

	// create correct output format
	s16leNormFile := path.Join(tmpDir, "s16le-norm."+path.Base(vidPath))
	{

		cmd := exec.Command("nice", "ffmpeg", "-y", "-loglevel", "quiet", "-i", normEbuWav, "-ar", strconv.Itoa(service.SampleRate), "-ac", "1", "-vn", "-f", "s16le", s16leNormFile)

		// TODO: capture and log instead
		cmd.Stdin = nil
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("ffmpeg command (s16le output) failed: %v", err)
		}

		log.Info().Str("video_path", vidPath).Msg("done converting .wav file to pcm_s16le")
	}

	// create correct output format
	audioFile := path.Join(path.Dir(vidPath), AudioFileName)
	{
		cmd := exec.Command("nice", "./build/bin/convert-to-discord-opus", "-i", s16leNormFile, "-o", audioFile)

		// TODO: capture and log instead
		cmd.Stdin = nil
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("convert-to-discord-opus command failed: %v", err)
		}

		log.Info().Str("video_path", vidPath).Msg("done converting pcm_s16le file to discord-opus")
	}

	err = os.RemoveAll(tmpDir)
	if err != nil {
		return fmt.Errorf("failed to ensure tmp dir was cleaned up: %s: %v", tmpDir, err)
	}

	return nil
}

func play(p *service.Player, url string, cacheDir string) {

	audioFile := path.Join(cacheDir, AudioFileName)

	p.Play(url, audioFile)
}
