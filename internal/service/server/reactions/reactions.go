package reactions

type ReactionStatus string

func (rs ReactionStatus) String() string {
	return string(rs)
}

const (
	ReactionStatusThinking ReactionStatus = "\U0001F914"
	ReactionStatusOK       ReactionStatus = "\u2705"
	ReactionStatusErr      ReactionStatus = "\u274C"
	ReactionStatusWarning  ReactionStatus = "\u26A0"
)

type warning struct {
	error
}

func NewWarning(err error) warning {
	return warning{err}
}

func (e warning) Error() string {
	err := e.error

	if err == nil {
		return ""
	}

	return err.Error()
}

func (e warning) Unwrap() error {
	return e.error
}

func (e warning) Reaction() ReactionStatus {
	return ReactionStatusWarning
}
