package wip

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCaptureRejectsEachUnmatchedSelectionBeforeStaging(t *testing.T) {
	for _, test := range []struct {
		name, unmatched string
		options         CaptureOptions
	}{
		{name: "typo", unmatched: "missing.txt", options: CaptureOptions{Paths: []string{"missing.txt"}}},
		{name: "mixed", unmatched: "missing.txt", options: CaptureOptions{Paths: []string{"tracked.txt", "missing.txt"}}},
		{name: "positional-message", unmatched: "old-style message", options: CaptureOptions{Paths: []string{"old-style message"}}},
		{name: "ignored", unmatched: "ignored.txt", options: CaptureOptions{Paths: []string{"ignored.txt"}}},
		{name: "tracked-only", unmatched: "new.txt", options: CaptureOptions{TrackedOnly: true, Paths: []string{"new.txt"}}},
		{name: "empty-directory", unmatched: "empty", options: CaptureOptions{Paths: []string{"empty"}}},
		{name: "literal", unmatched: "*.txt", options: CaptureOptions{Paths: []string{"*.txt"}}},
		{name: "baseline-mixed", unmatched: "missing.txt", options: CaptureOptions{Baseline: true, Paths: []string{"tracked.txt", "missing.txt"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newTestRepository(t, true)
			f.write(".gitignore", "ignored.txt\n")
			f.git("add", ".gitignore")
			f.git("commit", "-m", "ignore policy")
			r := openFixture(t, f)
			f.write("tracked.txt", "checkpoint change\n")
			saved := captureFixture(t, r, CaptureOptions{})
			f.write("tracked.txt", "staged\n")
			f.git("add", "tracked.txt")
			files := map[string]string{"tracked.txt": "working\n", "new.txt": "new\n", "ignored.txt": "ignored\n"}
			for path, contents := range files {
				f.write(path, contents)
			}
			if err := os.Mkdir(filepath.Join(f.dir, "empty"), 0700); err != nil {
				t.Fatal(err)
			}
			index, head, branch := f.read(".git/index"), f.git("rev-parse", "HEAD"), f.git("rev-parse", "refs/heads/main")
			// A nonexistent temp root also proves rejection precedes private-index allocation.
			t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "nonexistent"))
			result, err := r.Capture(context.Background(), test.options)
			requireErrorKind(t, err, KindInvalidOption)
			var detail *Error
			if !errors.As(err, &detail) || detail.Op != "capture" || !strings.Contains(detail.Message, strconv.Quote(test.unmatched)) || result != (CaptureResult{}) {
				t.Fatalf("rejected capture = %+v, %v", result, err)
			}
			if f.git("rev-parse", saved.Ref) != saved.Commit || f.git("rev-parse", "HEAD") != head || f.git("rev-parse", "refs/heads/main") != branch || !bytes.Equal(index, f.read(".git/index")) || f.git("show", ":tracked.txt") != "staged" {
				t.Fatal("invalid selection changed refs, HEAD, index, or staged content")
			}
			for path, contents := range files {
				if string(f.read(path)) != contents {
					t.Fatalf("invalid selection changed working file %s", path)
				}
			}
		})
	}
}

func TestCaptureUnchangedSelectionComparesCompleteSnapshot(t *testing.T) {
	f := newTestRepository(t, true)
	f.write("other.txt", "original other\n")
	f.git("add", "other.txt")
	f.git("commit", "-m", "second tracked file")
	r := openFixture(t, f)
	options := CaptureOptions{Paths: []string{"tracked.txt"}}
	unchanged := captureFixture(t, r, options)
	if unchanged.Created || unchanged.Parent != unchanged.HEAD {
		t.Fatalf("unchanged eligible file rejected or created checkpoint: %+v", unchanged)
	}
	f.write("other.txt", "changed other\n")
	changed := captureFixture(t, r, CaptureOptions{Paths: []string{"other.txt"}})
	// tracked.txt is unchanged, but omitting other.txt restores HEAD content in the snapshot.
	partial := captureFixture(t, r, options)
	if !partial.Created || partial.Parent != changed.Commit || f.git("rev-parse", partial.Commit+"^{tree}") != f.git("rev-parse", "HEAD^{tree}") || string(f.read("other.txt")) != "changed other\n" {
		t.Fatalf("unchanged selection failed to compare whole proposed snapshot: %+v", partial)
	}
	if repeated := captureFixture(t, r, options); repeated.Created || repeated.Parent != partial.Commit {
		t.Fatalf("unchanged selected snapshot advanced ref: %+v", repeated)
	}
}

func TestCaptureSelectedDeletedPathsFromSubdirectory(t *testing.T) {
	for _, selection := range []string{"gone/file space.txt", "gone"} {
		t.Run(selection, func(t *testing.T) {
			f := newTestRepository(t, true)
			f.write("nested/gone/file space.txt", "deleted content\n")
			f.write("nested/kept.txt", "kept content\n")
			f.git("add", ".")
			f.git("commit", "-m", "nested paths")
			if err := os.RemoveAll(filepath.Join(f.dir, "nested", "gone")); err != nil {
				t.Fatal(err)
			}
			r, err := Open(context.Background(), filepath.Join(f.dir, "nested"))
			if err != nil {
				t.Fatal(err)
			}
			saved := captureFixture(t, r, CaptureOptions{Paths: []string{selection}})
			if !saved.Created || f.git("diff-tree", "--no-commit-id", "--name-status", "-r", saved.Parent, saved.Commit) != "D\tnested/gone/file space.txt" {
				t.Fatalf("deleted selection did not capture deletion: %+v", saved)
			}
			if repeated := captureFixture(t, r, CaptureOptions{Paths: []string{selection}}); repeated.Created {
				t.Fatal("repeated deleted selection created a checkpoint")
			}
			_, err = r.Capture(context.Background(), CaptureOptions{Paths: []string{"kept.txt", "./missing.txt"}})
			requireErrorKind(t, err, KindInvalidOption)
			if !strings.Contains(err.Error(), strconv.Quote("nested/missing.txt")) || f.git("rev-parse", saved.Ref) != saved.Commit {
				t.Fatalf("subdirectory rejection did not identify normalized selection or changed ref: %v", err)
			}
		})
	}
}

func TestCaptureOmittedSelectionWithEmptyEligibleSet(t *testing.T) {
	f := newTestRepository(t, false)
	f.git("commit", "--allow-empty", "-m", "empty base")
	r := openFixture(t, f)
	if unchanged := captureFixture(t, r, CaptureOptions{}); unchanged.Created {
		t.Fatal("empty unfiltered capture created a checkpoint")
	}
	if baseline := captureFixture(t, r, CaptureOptions{Baseline: true}); !baseline.Created {
		t.Fatal("empty unfiltered baseline did not create a checkpoint")
	}
	_, err := r.Capture(context.Background(), CaptureOptions{Paths: []string{"."}})
	requireErrorKind(t, err, KindInvalidOption)
}
