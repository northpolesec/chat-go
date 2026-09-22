// Ported from packages/adapter-slack/src/blocks/errors.ts @ 6adca36 (chat v4.40.0).
// Divergences: TS Error subclass → pointer struct with Error(); name is
// implied by the type.
package blocks

// SlackBlockError is thrown for an unsupported Slack card element.
type SlackBlockError struct {
	msg string
}

func (e *SlackBlockError) Error() string {
	if e == nil {
		return ""
	}
	return e.msg
}

func newSlackBlockError(message string) *SlackBlockError {
	return &SlackBlockError{msg: message}
}
