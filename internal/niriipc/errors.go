package niriipc

import (
	"fmt"

	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

type ObservationError struct {
	Code sliceprotocol.ReasonCode
	Err  error
}

func (e *ObservationError) Error() string {
	if e.Err == nil {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}
func (e *ObservationError) Unwrap() error { return e.Err }

func reason(code sliceprotocol.ReasonCode, err error) error {
	return &ObservationError{Code: code, Err: err}
}

func ReasonCode(err error) sliceprotocol.ReasonCode {
	for err != nil {
		if observation, ok := err.(*ObservationError); ok {
			return observation.Code
		}
		type unwrapper interface{ Unwrap() error }
		value, ok := err.(unwrapper)
		if !ok {
			break
		}
		err = value.Unwrap()
	}
	return sliceprotocol.ReasonNiriMalformed
}
