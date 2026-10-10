package wip

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCapturePathBoundariesAndSymlinks(t *testing.T) {
	f := newTestRepository(t, true)
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "secret"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(f.dir, "link")); err != nil {
		t.Fatal(err)
	}
	f.git("add", "link")
	r := openFixture(t, f)
	for _, path := range []string{"../outside", out, "link/secret", "", "invalid\x00path"} {
		_, err := r.Capture(context.Background(), CaptureOptions{Paths: []string{path}})
		requireErrorKind(t, err, KindInvalidOption)
	}
	link := captureFixture(t, r, CaptureOptions{Paths: []string{"link"}})
	if f.git("show", link.Commit+":link") != out {
		t.Fatal("tracked symlink contents were not preserved")
	}
}
