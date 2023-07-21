package reactions

import (
	"fmt"
)

type Status uint8

const StatusZeroValue Status = 0

const (
	StatusUndefinedLower Status = iota
	//
	StatusThinking
	StatusOK
	StatusErr
	StatusWarning
	//
	StatusUndefinedUpper
)

func (s Status) String() string {

	// m := map[ReactionStatus]string{
	// 	ReactionStatusThinking: "\U0001F914",
	// 	ReactionStatusOK:       "\u2705",
	// 	ReactionStatusErr:      "\u274C",
	// 	ReactionStatusWarning:  "\u26A0",
	// }

	if s <= StatusUndefinedLower || s >= StatusUndefinedUpper {
		return fmt.Sprintf("ReactionStatus(%d)", s)
	}

	return []string{
		"\U0001F914",
		"\u2705",
		"\u274C",
		"\u26A0",
	}[s-1]
}

type ReactionError struct {
	err error
	rs  Status
}

func NewWarning(err error) *ReactionError {
	return newReactionError(err, StatusWarning)
}

func newReactionError(err error, rs Status) *ReactionError {
	return &ReactionError{err, rs}
}

func (e *ReactionError) Error() string {
	err := e.err

	if err == nil {
		return "ReactionError: unknown error"
	}

	return err.Error()
}

func (e *ReactionError) Unwrap() error {
	return e.err
}

func (e *ReactionError) Reaction() Status {
	return e.rs
}

type Reactor interface {
	Reaction() Status
}

//nolint:errcheck
var _ Reactor = newReactionError(nil, StatusZeroValue)
