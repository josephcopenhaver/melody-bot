package reactions

import (
	"fmt"
)

type ReactionStatus uint8

const ReactionStatusZeroValue ReactionStatus = 0

const (
	ReactionStatusUndefinedLower ReactionStatus = iota
	//
	ReactionStatusThinking
	ReactionStatusOK
	ReactionStatusErr
	ReactionStatusWarning
	//
	ReactionStatusUndefinedUpper
)

func (rs ReactionStatus) String() string {

	// m := map[ReactionStatus]string{
	// 	ReactionStatusThinking: "\U0001F914",
	// 	ReactionStatusOK:       "\u2705",
	// 	ReactionStatusErr:      "\u274C",
	// 	ReactionStatusWarning:  "\u26A0",
	// }

	if rs <= ReactionStatusUndefinedLower || rs >= ReactionStatusUndefinedUpper {
		return fmt.Sprintf("ReactionStatus(%d)", rs)
	}

	return []string{
		"\U0001F914",
		"\u2705",
		"\u274C",
		"\u26A0",
	}[rs-1]
}

type errWithReaction struct {
	err error
	rs  ReactionStatus
}

func NewWarning(err error) *errWithReaction {
	return newErrWithReaction(err, ReactionStatusWarning)
}

func newErrWithReaction(err error, rs ReactionStatus) *errWithReaction {
	return &errWithReaction{err, rs}
}

func (e *errWithReaction) Error() string {
	err := e.err

	if err == nil {
		return "errWithReaction: unknown error"
	}

	return err.Error()
}

func (e *errWithReaction) Unwrap() error {
	return e.err
}

func (e *errWithReaction) Reaction() ReactionStatus {
	return e.rs
}

type Reactor interface {
	Reaction() ReactionStatus
}

var _ Reactor = newErrWithReaction(nil, ReactionStatusZeroValue)
