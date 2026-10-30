package cli

import (
	"context"
	"errors"

	"github.com/otnc/vmeltype/internal/appctl"
	"github.com/otnc/vmeltype/internal/lifecycle"
	"github.com/otnc/vmeltype/internal/prompt"
)

// 終了コード。scripts から結果を区別できるよう、意味を固定する。
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitNeedsAction = 3
	// ExitUpdateAvailable は check だけが返す。
	ExitUpdateAvailable = 10
	// ExitCanceled は、変更を始める前の中止に使う。
	ExitCanceled = 130
)

type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func exitCodeOf(err error) int {
	var usage usageError
	switch {
	case err == nil:
		return ExitOK
	case errors.As(err, &usage):
		return ExitUsage
	case errors.Is(err, prompt.ErrNeedsConfirmation), errors.Is(err, errAppRunning),
		errors.Is(err, lifecycle.ErrPendingJournal), errors.Is(err, lifecycle.ErrNeedsRecover), errors.Is(err, lifecycle.ErrLocked),
		errors.Is(err, lifecycle.ErrPinned), errors.Is(err, appctl.ErrStillRunning):
		return ExitNeedsAction
	case errors.Is(err, prompt.ErrCanceled), errors.Is(err, context.Canceled):
		return ExitCanceled
	default:
		return ExitError
	}
}
