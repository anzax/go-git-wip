package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	wip "github.com/anzax/go-git-wip"
)

// These are caller-owned records, not library types. A real host persists them
// outside the captured worktree and records failed attempts separately.
type endpoint struct {
	OID        string    `json:"oid"`
	HEAD       string    `json:"head"`
	Created    bool      `json:"created"`
	CapturedAt time.Time `json:"captured_at"`
}

type turnTrace struct {
	SessionID string   `json:"session_id"`
	TurnID    string   `json:"turn_id"`
	Worktree  string   `json:"worktree"`
	Start     endpoint `json:"start"`
	End       endpoint `json:"end"`
	Status    string   `json:"status"`
}

func captureEndpoint(ctx context.Context, repository *wip.Repository, message string, baseline bool) (endpoint, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := repository.Capture(ctx, wip.CaptureOptions{
		Paths: nil, TrackedOnly: false, IncludeIgnored: false,
		Baseline: baseline, Message: message,
	})
	if err != nil {
		return endpoint{}, err // Never manufacture an endpoint for a failed capture.
	}
	oid := result.Commit
	if !result.Created {
		oid = result.Parent
	}
	return endpoint{OID: oid, HEAD: result.HEAD, Created: result.Created, CapturedAt: time.Now().UTC()}, nil
}

// runSession demonstrates sequential hook boundaries in the existing disposable
// repository. It does not install hooks, acquire host locks, or run an agent.
func runSession(ctx context.Context, repository *wip.Repository, root string, git func(...string) (string, error)) error {
	write := func(name, content string) error {
		return os.WriteFile(filepath.Join(root, name), []byte(content), 0600)
	}
	// Work already present belongs to the baseline, not the first turn's diff.
	if err := write("session-existing.txt", "before the session\n"); err != nil {
		return err
	}
	baseline, err := captureEndpoint(ctx, repository, "session demo: baseline", true)
	if err != nil {
		return err
	}
	if !baseline.Created {
		return fmt.Errorf("forced session baseline was not created")
	}
	info, err := repository.Info(ctx)
	if err != nil {
		return err
	}
	var traces []turnTrace
	for turn := 1; turn <= 3; turn++ {
		if turn == 2 {
			// A manual save or another session can intervene between our turns.
			if err := write("session-outside.txt", "between turns\n"); err != nil {
				return err
			}
			if _, err := captureEndpoint(ctx, repository, "outside capture", false); err != nil {
				return err
			}
		}
		if turn == 3 {
			// Normal commits roll the checkpoint stack over. Explicit endpoints
			// still work, and a no-change start can refer directly to normal HEAD.
			for _, args := range [][]string{{"add", "--all"}, {"commit", "-m", "commit between turns"}} {
				if _, err := git(args...); err != nil {
					return err
				}
			}
		}
		trace := turnTrace{SessionID: "demo", TurnID: fmt.Sprint(turn), Worktree: info.Root}
		trace.Start, err = captureEndpoint(ctx, repository, "session demo turn "+trace.TurnID+": start", false)
		if err != nil {
			return err
		}
		switch turn {
		case 1:
			err = write("session-turn.txt", "turn one\n")
		case 2: // An unchanged turn still needs a trace record.
		case 3:
			err = write("session-turn.txt", "turn one\nturn three\n")
		}
		if err != nil {
			return err
		}
		trace.End, err = captureEndpoint(ctx, repository, "session demo turn "+trace.TurnID+": end", false)
		if err != nil {
			return err
		}
		trace.Status = "completed"
		comparison, err := repository.Compare(ctx, trace.Start.OID, trace.End.OID, wip.CompareOptions{})
		if err != nil {
			return err
		}
		if turn == 2 {
			if len(comparison.Files) != 0 || trace.Start.Created || trace.End.Created || trace.Start.OID != trace.End.OID || trace.Start.OID == traces[0].End.OID {
				return fmt.Errorf("unchanged turn did not retain its own boundaries: %+v", trace)
			}
		} else {
			status := "A"
			if turn == 3 {
				status = "M"
			}
			want := wip.FileChange{Path: "session-turn.txt", Status: status, Added: 1}
			if !trace.End.Created || len(comparison.Files) != 1 || comparison.Files[0] != want {
				return fmt.Errorf("turn %d comparison: %+v", turn, comparison)
			}
		}
		if turn == 3 && (trace.Start.Created || trace.Start.OID != trace.Start.HEAD || trace.Start.HEAD == baseline.HEAD) {
			return fmt.Errorf("normal-commit boundary was not retained: %+v", trace)
		}
		traces = append(traces, trace)
	}
	comparison, err := repository.Compare(ctx, baseline.OID, traces[2].End.OID, wip.CompareOptions{})
	if err != nil {
		return err
	}
	// The whole-session net diff includes outside work but excludes pre-session
	// content: session boundaries are not proof of exclusive agent authorship.
	if len(comparison.Files) != 2 || comparison.Files[0] != (wip.FileChange{Path: "session-outside.txt", Status: "A", Added: 1}) ||
		comparison.Files[1] != (wip.FileChange{Path: "session-turn.txt", Status: "A", Added: 2}) {
		return fmt.Errorf("session comparison: %+v", comparison)
	}
	fmt.Printf("Session baseline: %s\nTurn traces (changed, unchanged, after normal commit):\n", baseline.OID)
	encoder := json.NewEncoder(os.Stdout)
	for _, trace := range traces {
		if err := encoder.Encode(trace); err != nil {
			return err
		}
	}
	return nil
}
