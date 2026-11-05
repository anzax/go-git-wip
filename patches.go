package wip

import (
	"context"
	"strings"
)

// PatchFile contains the patch for one normalized literal path selection.
type PatchFile struct {
	Path  string `json:"path"`
	Patch string `json:"patch"`
}

// Patches retains the exact resolved endpoints and stable deduplicated paths.
type Patches struct {
	From  string      `json:"from"`
	To    string      `json:"to"`
	Files []PatchFile `json:"files"`
}

// ComparePatches resolves the endpoints once and generates only requested
// patches, without computing unused status or line-count summaries. Paths are
// explicit literal selections relative to the invocation directory. As with
// Compare, directory selections may include descendants. When configured,
// MaxStdoutBytes also bounds the combined returned patches. The from value
// "empty" is accepted only when to resolves to a verified root commit.
func (r *Repository) ComparePatches(ctx context.Context, from, to string, paths []string) (Patches, error) {
	out := Patches{Files: []PatchFile{}}
	if len(paths) == 0 {
		return out, &Error{Kind: KindInvalidOption, Op: "compare-patches", Message: "explicit paths are required"}
	}
	selected, err := r.normalizePaths(paths)
	if err != nil {
		return out, err
	}
	if from != "empty" {
		if out.From, err = r.resolveCommit(ctx, from); err != nil {
			return out, err
		}
	} else {
		out.From = "empty"
	}
	if out.To, err = r.resolveCommit(ctx, to); err != nil {
		return out, err
	}
	if from == "empty" {
		objects, err := r.batchObjects(ctx, []string{out.To}, true)
		if err != nil {
			return out, err
		}
		object := objects[out.To]
		headers, _, found := strings.Cut(string(object.raw), "\n\n")
		if object.kind != "commit" || !found {
			return out, &Error{Kind: KindInvalidOption, Op: "compare-patches", Message: "empty baseline requires a readable root commit"}
		}
		for _, header := range strings.Split(headers, "\n") {
			if strings.HasPrefix(header, "parent ") {
				return out, &Error{Kind: KindInvalidOption, Op: "compare-patches", Message: "empty baseline requires a root commit"}
			}
		}
	}
	var total int64
	for _, path := range selected {
		var patch []byte
		var err error
		if from == "empty" {
			patch, err = r.git(ctx, "diff-tree", "--root", "--no-commit-id", "-p", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", out.To, "--", path)
		} else {
			patch, err = r.git(ctx, "diff", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", "--patch", out.From, out.To, "--", path)
		}
		if err != nil {
			return out, err
		}
		total += int64(len(patch))
		if r.maxStdoutBytes > 0 && total > r.maxStdoutBytes {
			return out, &Error{Kind: KindOutputLimit, Op: "compare-patches", Message: "combined patches exceed the output byte limit"}
		}
		out.Files = append(out.Files, PatchFile{Path: path, Patch: string(patch)})
	}
	return out, nil
}
