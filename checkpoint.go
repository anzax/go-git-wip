package wip

import (
	"bytes"
	"context"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

func checkpointRef(branch string) string { return "refs/wip/" + branch }

func (r *Repository) validateCheckpointRef(ctx context.Context, ref string) (string, error) {
	if !strings.HasPrefix(ref, "refs/wip/") || strings.TrimPrefix(ref, "refs/wip/") == "" {
		return "", &Error{Kind: KindInvalidRef, Op: "ref", Message: "expected a complete refs/wip/ ref"}
	}
	if _, err := r.git(ctx, "check-ref-format", ref); err != nil {
		if ctx.Err() != nil || outputLimitError(err) {
			return "", err
		}
		return "", &Error{Kind: KindInvalidRef, Op: "ref", Message: "invalid checkpoint ref name", Err: err}
	}
	return strings.TrimPrefix(ref, "refs/wip/"), nil
}

func (r *Repository) resolveCommit(ctx context.Context, revision string) (string, error) {
	if revision == "" || strings.ContainsRune(revision, 0) {
		return "", &Error{Kind: KindInvalidOption, Op: "revision", Message: "a commit revision is required"}
	}
	out, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", revision+"^{commit}")
	if err != nil {
		if gitExitCode(err, 1) {
			return "", &Error{Kind: KindNotFound, Op: "revision", Message: "commit revision not found", Err: err}
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *Repository) fullObjectID(ctx context.Context, oid string) (bool, error) {
	out, err := r.git(ctx, "rev-parse", "--show-object-format")
	if err != nil {
		return false, err
	}
	length := 40
	switch strings.TrimSpace(string(out)) {
	case "sha1":
	case "sha256":
		length = 64
	default:
		return false, &Error{Kind: KindUnsupported, Op: "object", Message: "unsupported Git object format"}
	}
	if len(oid) != length || oid != strings.ToLower(oid) {
		return false, nil
	}
	_, err = hex.DecodeString(oid)
	return err == nil, nil
}

func (r *Repository) resolveCheckpoint(ctx context.Context, revision string) (Checkpoint, error) {
	oid, err := r.resolveCommit(ctx, revision)
	if err != nil {
		return Checkpoint{}, err
	}
	raw, err := r.git(ctx, "cat-file", "commit", oid)
	if err != nil {
		return Checkpoint{}, err
	}
	return r.decodeCheckpoint(ctx, oid, raw)
}

func (r *Repository) decodeCheckpoint(ctx context.Context, oid string, raw []byte) (Checkpoint, error) {
	invalid := func(message string) (Checkpoint, error) {
		return Checkpoint{}, &Error{Kind: KindInvalidRef, Op: "checkpoint", Message: message}
	}
	headers, body, found := bytes.Cut(raw, []byte("\n\n"))
	if !found {
		return invalid("checkpoint commit has no message")
	}
	meta, message, err := decodeCheckpointMessage(string(body))
	if err != nil {
		return invalid(err.Error())
	}
	if _, err := r.validateCheckpointRef(ctx, checkpointRef(meta.Branch)); err != nil {
		return Checkpoint{}, err
	}
	valid, err := r.fullObjectID(ctx, meta.Base)
	if err != nil {
		return Checkpoint{}, err
	}
	if !valid {
		return invalid("checkpoint base must be a full commit object ID")
	}
	base, err := r.resolveCommit(ctx, meta.Base)
	if err != nil {
		if ctx.Err() != nil || outputLimitError(err) {
			return Checkpoint{}, err
		}
		return invalid("checkpoint base commit is unavailable")
	}
	if base != meta.Base {
		return invalid("checkpoint base is not a direct commit ID")
	}
	cp := Checkpoint{Commit: oid, Branch: meta.Branch, Base: meta.Base, Message: message}
	parents := 0
	committer := false
	for _, header := range strings.Split(string(headers), "\n") {
		if strings.HasPrefix(header, "parent ") {
			parents++
			cp.Parent = strings.TrimPrefix(header, "parent ")
		}
		if strings.HasPrefix(header, "committer ") {
			fields := strings.Fields(header)
			if len(fields) < 4 || committer {
				return invalid("invalid checkpoint committer")
			}
			seconds, err := strconv.ParseInt(fields[len(fields)-2], 10, 64)
			if err != nil {
				return invalid("invalid checkpoint timestamp")
			}
			cp.Time, committer = time.Unix(seconds, 0).UTC(), true
		}
	}
	if parents != 1 || !committer {
		return invalid("checkpoint must have one parent and a committer")
	}
	parent, err := r.resolveCommit(ctx, cp.Parent)
	if err != nil {
		if ctx.Err() != nil || outputLimitError(err) {
			return Checkpoint{}, err
		}
		return invalid("checkpoint parent commit is unavailable")
	}
	if parent != cp.Parent {
		return invalid("checkpoint parent must be a full direct commit ID")
	}
	return cp, nil
}

func (r *Repository) validateCheckpointParent(ctx context.Context, cp Checkpoint, op string) (Checkpoint, error) {
	if cp.Parent == cp.Base {
		return Checkpoint{}, nil
	}
	parent, err := r.resolveCheckpoint(ctx, cp.Parent)
	if err != nil {
		return Checkpoint{}, err
	}
	if parent.Branch != cp.Branch || parent.Base != cp.Base {
		return Checkpoint{}, &Error{Kind: KindInvalidRef, Op: op, Message: "checkpoint parent belongs to another stack"}
	}
	return parent, nil
}

func (r *Repository) validateTip(ctx context.Context, ref, expectedBranch string) (Checkpoint, error) {
	branch, err := r.validateCheckpointRef(ctx, ref)
	if err != nil {
		return Checkpoint{}, err
	}
	if branch != expectedBranch {
		return Checkpoint{}, &Error{Kind: KindInvalidRef, Op: "ref", Message: "checkpoint ref does not match the expected branch"}
	}
	if _, err := r.git(ctx, "symbolic-ref", "--quiet", ref); err == nil {
		return Checkpoint{}, &Error{Kind: KindInvalidRef, Op: "ref", Message: "symbolic checkpoint refs are unsupported"}
	} else if !gitExitCode(err, 1) {
		if ctx.Err() != nil || outputLimitError(err) {
			return Checkpoint{}, err
		}
		return Checkpoint{}, &Error{Kind: KindInvalidRef, Op: "ref", Message: "checkpoint ref cannot be resolved", Err: err}
	}
	// Listing all direct refs detects malformed loose refs that a quiet verification
	// otherwise reports indistinguishably from absence. Match the full name exactly.
	refs, err := r.git(ctx, "show-ref")
	if err != nil && !gitExitCode(err, 1) {
		if ctx.Err() != nil || outputLimitError(err) {
			return Checkpoint{}, err
		}
		return Checkpoint{}, &Error{Kind: KindInvalidRef, Op: "ref", Message: "repository contains an invalid ref", Err: err}
	}
	var tip string
	for _, line := range strings.Split(string(refs), "\n") {
		oid, name, found := strings.Cut(line, " ")
		if found && name == ref {
			tip = oid
			break
		}
	}
	if tip == "" {
		return Checkpoint{}, nil
	}
	cp, err := r.resolveCheckpoint(ctx, tip)
	if err != nil {
		if ctx.Err() != nil || outputLimitError(err) {
			return Checkpoint{}, err
		}
		return Checkpoint{}, &Error{Kind: KindInvalidRef, Op: "ref", Message: "invalid checkpoint ref target", Err: err}
	}
	if cp.Commit != tip {
		return Checkpoint{}, &Error{Kind: KindInvalidRef, Op: "ref", Message: "checkpoint ref must point directly to a commit"}
	}
	if cp.Branch != branch {
		return Checkpoint{}, &Error{Kind: KindInvalidRef, Op: "ref", Message: "checkpoint metadata does not match its branch"}
	}
	if _, err := r.validateCheckpointParent(ctx, cp, "ref"); err != nil {
		return Checkpoint{}, err
	}
	return cp, nil
}

func checkpointIdentity() []string {
	date := time.Now().Format(time.RFC3339)
	return []string{
		"GIT_AUTHOR_NAME=WIP checkpoint", "GIT_AUTHOR_EMAIL=wip@localhost",
		"GIT_COMMITTER_NAME=WIP checkpoint", "GIT_COMMITTER_EMAIL=wip@localhost",
		"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date,
	}
}
