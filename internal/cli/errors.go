package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/anzax/go-git-wip"
)

type usageError struct{ message string }

func (e *usageError) Error() string { return e.message }

type partialCleanupError struct{ count int }

func (e *partialCleanupError) Error() string {
	return fmt.Sprintf("cleanup failed for %d target(s); see the reported failures", e.count)
}

// Run executes one invocation and writes its diagnostic once. The caller owns
// signal handling and process exits; commands never read stdin for confirmation.
func Run(ctx context.Context, version string, args []string, stdout, stderr io.Writer) int {
	cmd, opts := newCommand(version)
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	_, err := cmd.ExecuteContextC(ctx)
	if err == nil {
		return 0
	}
	kind, code := errorCategory(err, opts.started)
	jsonOutput := opts.json
	if !opts.started {
		jsonOutput = requestedJSON(args, jsonOutput)
	}
	if jsonOutput {
		value := struct {
			Error struct {
				Kind    string `json:"kind"`
				Message string `json:"message"`
			} `json:"error"`
		}{}
		value.Error.Kind, value.Error.Message = kind, err.Error()
		_ = json.NewEncoder(stderr).Encode(value)
	} else {
		_, _ = fmt.Fprintln(stderr, err)
	}
	return code
}

func errorCategory(err error, started bool) (string, int) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled", 130
	}
	var usage *usageError
	if errors.As(err, &usage) || !started {
		return "usage", 2
	}
	var failure *wip.Error
	if errors.As(err, &failure) {
		switch failure.Kind {
		case wip.KindInvalidOption:
			return string(failure.Kind), 2
		case wip.KindConflict:
			return string(failure.Kind), 3
		default:
			return string(failure.Kind), 1
		}
	}
	return "failure", 1
}

// Parsing can fail before Cobra reaches a later --json flag. Honor that explicit
// output request too, while leaving positional paths after -- alone.
func requestedJSON(args []string, parsed bool) bool {
	requested := parsed
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--json" || arg == "--json=true" {
			requested = true
		} else if strings.HasPrefix(arg, "--json=") && strings.TrimPrefix(arg, "--json=") == "false" {
			requested = false
		}
	}
	return requested
}
