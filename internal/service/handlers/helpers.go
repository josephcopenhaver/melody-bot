package handlers

import (
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

type HandleMessageCreate struct {
	Name        string
	Usage       string
	Description string
	Matcher     func(*service.Player, string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error
}

func newHandleMessageCreate(name, usage, description string, matcher func(*service.Player, string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error) HandleMessageCreate {
	return HandleMessageCreate{
		Name:        name,
		Usage:       usage,
		Description: description,
		Matcher:     matcher,
	}
}

func newWordMatcher(requirePlayer bool, words []string, handler func(*discordgo.Session, *discordgo.MessageCreate, *service.Player, map[string]string) error) func(*service.Player, string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error {
	return func(p *service.Player, s string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error {

		if p == nil && requirePlayer {
			return nil
		}

		s = strings.TrimSpace(s)
		for _, w := range words {
			if w == s {
				return func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {
					return handler(s, m, p, nil)
				}
			}
		}

		return nil
	}
}

func newRegexMatcher(requirePlayer bool, re *regexp.Regexp, handler func(*discordgo.Session, *discordgo.MessageCreate, *service.Player, map[string]string) error) func(*service.Player, string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error {
	return func(p *service.Player, s string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error {

		if p == nil && requirePlayer {
			return nil
		}

		args := regexMap(re, s)
		if args == nil {
			return nil
		}

		return func(s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {
			return handler(s, m, p, args)
		}
	}
}

func regexMap(r *regexp.Regexp, s string) map[string]string {

	args := r.FindStringSubmatch(s)
	if args == nil {
		return nil
	}

	names := r.SubexpNames()
	m := make(map[string]string, len(args))

	for i, v := range args {
		m[names[i]] = v
	}

	return m
}
