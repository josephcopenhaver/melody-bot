package handlers

import (
	"context"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/josephcopenhaver/melody-bot/internal/service"
)

type HandleMessageCreate struct {
	Name        string
	Usage       string
	Description string
	Matcher     func(*service.Player, string) func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player, *service.Brain) error
}

func newHandleMessageCreate(name, usage, description string, matcher func(*service.Player, string) func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player) error) HandleMessageCreate {
	return HandleMessageCreate{
		Name:        name,
		Usage:       usage,
		Description: description,
		Matcher: func(p *service.Player, s string) func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player, *service.Brain) error {
			f := matcher(p, s)
			if f == nil {
				return nil
			}

			return func(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, _ *service.Brain) error {
				return f(ctx, s, m, p)
			}
		},
	}
}

func newHandleMessageCreateWithBrain(name, usage, description string, matcher func(*service.Player, string) func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player, *service.Brain) error) HandleMessageCreate {
	return HandleMessageCreate{
		Name:        name,
		Usage:       usage,
		Description: description,
		Matcher:     matcher,
	}
}

func newWordMatcher(requirePlayer bool, words []string, handler func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player) error) func(*service.Player, string) func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player) error {
	return func(p *service.Player, s string) func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player) error {

		if p == nil && requirePlayer {
			return nil
		}

		s = strings.TrimSpace(s)
		for _, w := range words {
			if w == s {
				return func(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {
					return handler(ctx, s, m, p)
				}
			}
		}

		return nil
	}
}

func newRegexMatcher(requirePlayer bool, re *regexp.Regexp, handler func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player, map[string]string) error) func(*service.Player, string) func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player) error {
	return func(p *service.Player, s string) func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player) error {

		if p == nil && requirePlayer {
			return nil
		}

		args := regexMap(re, s)
		if args == nil {
			return nil
		}

		return func(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player) error {
			return handler(ctx, s, m, p, args)
		}
	}
}

func newRegexMatcherWithBrain(requirePlayer bool, re *regexp.Regexp, handler func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player, map[string]string, *service.Brain) error) func(*service.Player, string) func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player, *service.Brain) error {
	return func(p *service.Player, s string) func(context.Context, *discordgo.Session, *discordgo.MessageCreate, *service.Player, *service.Brain) error {

		if p == nil && requirePlayer {
			return nil
		}

		args := regexMap(re, s)
		if args == nil {
			return nil
		}

		return func(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, p *service.Player, b *service.Brain) error {
			return handler(ctx, s, m, p, args, b)
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
