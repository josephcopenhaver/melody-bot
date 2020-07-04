package handlers

import (
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/discord-bot/internal/service"
)

type HandleMessageCreate struct {
	Name    string
	Matcher func(string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error
}

func newHandleMessageCreate(name string, matcher func(string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error) HandleMessageCreate {
	return HandleMessageCreate{
		Name:    name,
		Matcher: matcher,
	}
}

func newWordMatcher(words []string, handler func(*discordgo.Session, *discordgo.MessageCreate, *service.Player, map[string]string) error) func(string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error {
	return func(s string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error {

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

func newRegexMatcher(re *regexp.Regexp, handler func(*discordgo.Session, *discordgo.MessageCreate, *service.Player, map[string]string) error) func(string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error {
	return func(s string) func(*discordgo.Session, *discordgo.MessageCreate, *service.Player) error {

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
