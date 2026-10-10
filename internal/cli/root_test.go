package cli

import (
	"bytes"
	"strings"
	"testing"
)

// Informational commands must work without opening a repository.
func TestHelpAndVersionOutsideRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{}, {"help"}, {"--help"}, {"-h"}, {"version"}, {"--version"}, {"-v"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			cmd := NewCommand("test-version")
			cmd.SetArgs(args)
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			want := "Usage:"
			if len(args) > 0 && (strings.Contains(args[0], "version") || args[0] == "-v") {
				want = "test-version\n"
			}
			if !strings.Contains(out.String(), want) {
				t.Fatalf("output %q does not contain %q", out.String(), want)
			}
		})
	}
}
