package wip

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Hash a real commit directly so Git does not repair malformed message framing.
// --literally permits deliberate invalid-object fixtures, including NUL bytes.
func commitWithRawMessage(t *testing.T, f *testRepository, parent, message string) string {
	t.Helper()
	r := openFixture(t, f)
	raw := fmt.Sprintf("tree %s\nparent %s\nauthor Fixture <fixture@localhost> 1767268800 +0000\ncommitter Fixture <fixture@localhost> 1767268800 +0000\n\n%s",
		f.git("rev-parse", "HEAD^{tree}"), parent, message)
	out, err := r.gitEnv(context.Background(), nil, []byte(raw), "hash-object", "--literally", "-t", "commit", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestCheckpointV2RawSubjectsAndExactMessageRoundTrips(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head := f.git("rev-parse", "HEAD")
	legacyLooking := fmt.Sprintf("go-git-wip:1 {\"branch\":\"main\",\"base\":\"%s\"}\n\nlegacy-looking caller body\n", head)
	for _, test := range []struct {
		name, message, subject string
		baseline               bool
	}{
		{name: "subject", message: "agent turn 4: edit parser", subject: "agent turn 4: edit parser", baseline: true},
		{name: "multiline", message: "review parser\n\nbody\nwith details", subject: "review parser", baseline: true},
		{name: "trailing-newlines", message: "review parser\n\nbody\n\n\n", subject: "review parser", baseline: true},
		{name: "whitespace", message: "  review parser  \n\n body \t\r\n", subject: "  review parser", baseline: true},
		{name: "unicode-bytes", message: "修正: żółw 🐢\n\ndétails\n", subject: "修正: żółw 🐢", baseline: true},
		{name: "embedded-footer", message: "human subject\n\ngo-git-wip:2 {\"branch\":\"other\",\"base\":\"fake\",\"message_bytes\":1}\n", subject: "human subject", baseline: true},
		{name: "legacy-looking-leading-message", message: legacyLooking, subject: "Checkpoint: " + strings.Split(legacyLooking, "\n")[0], baseline: true},
		{name: "unknown-leading-namespace", message: "go-git-wip:99 caller text", subject: "Checkpoint: go-git-wip:99 caller text", baseline: true},
		{name: "ordinary-escape-text", message: "Checkpoint: go-git-wip:99 caller text", subject: "Checkpoint: go-git-wip:99 caller text", baseline: true},
		{name: "default-baseline", subject: "Baseline", baseline: true},
		{name: "default-checkpoint", subject: "Checkpoint"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !test.baseline {
				f.write("tracked.txt", "changed\n")
			}
			saved := captureFixture(t, r, CaptureOptions{Baseline: test.baseline, Message: test.message})
			if !saved.Created || f.git("log", "-1", "--format=%s", saved.Commit) != test.subject {
				t.Fatalf("unexpected Git subject: %q", f.git("log", "-1", "--format=%s", saved.Commit))
			}
			message := test.message
			if message == "" {
				message = test.subject
			}
			display := message
			if strings.HasPrefix(display, "go-git-wip:") {
				display = "Checkpoint: " + display
			}
			wantBody := fmt.Sprintf("%s\n\ngo-git-wip:2 {\"branch\":\"main\",\"base\":\"%s\",\"message_bytes\":%d}\n", display, head, len(message))
			raw, err := r.git(context.Background(), "cat-file", "commit", saved.Commit)
			if err != nil {
				t.Fatal(err)
			}
			_, body, found := bytes.Cut(raw, []byte("\n\n"))
			if !found || string(body) != wantBody {
				t.Fatalf("raw body = %q, want %q", body, wantBody)
			}
			history, err := r.History(context.Background(), HistoryOptions{Limit: 1})
			if err != nil || len(history) != 1 || history[0].Message != message || history[0].Base != head || history[0].Branch != "main" {
				t.Fatalf("message did not roundtrip: %+v, %v", history, err)
			}
			inspection, err := r.Inspect(context.Background(), saved.Commit, CompareOptions{})
			if err != nil || inspection.From != saved.Parent || inspection.To != saved.Commit {
				t.Fatalf("v2 inspection = %+v, %v", inspection, err)
			}
		})
	}
	_, err := r.Capture(context.Background(), CaptureOptions{Baseline: true, Message: "bad\x00message"})
	requireErrorKind(t, err, KindInvalidOption)
}

func TestCheckpointV1FooterTextAndMixedVersionOperations(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head := f.git("rev-parse", "HEAD")
	// This complete valid-looking v2 envelope is caller text inside a legacy message.
	legacyMessage := fmt.Sprintf("legacy human message\n\ngo-git-wip:2 {\"branch\":\"main\",\"base\":\"%s\",\"message_bytes\":20}\n", head)
	legacyBody := fmt.Sprintf("go-git-wip:1 {\"branch\":\"main\",\"base\":\"%s\"}\n\n%s\n", head, legacyMessage)
	legacy := commitWithRawMessage(t, f, head, legacyBody)
	f.git("update-ref", "refs/wip/main", legacy)
	if unchanged := captureFixture(t, r, CaptureOptions{}); unchanged.Created || unchanged.Parent != legacy || f.git("rev-parse", "refs/wip/main") != legacy {
		t.Fatalf("unchanged capture rewrote legacy tip: %+v", unchanged)
	}
	f.write("tracked.txt", "modern working content\n")
	modern := captureFixture(t, r, CaptureOptions{Message: "modern subject\n\nmodern body\n"})
	if modern.Parent != legacy || modern.Ref != "refs/wip/main" {
		t.Fatalf("v2 did not continue legacy stack: %+v", modern)
	}
	// A v1 child of a v2 entry exercises reading transitions in both directions.
	legacyChildMessage := "legacy child\n\ntrailing\n\n"
	legacyChild := commitWithRawMessage(t, f, modern.Commit, fmt.Sprintf("go-git-wip:1 {\"branch\":\"main\",\"base\":\"%s\"}\n\n%s\n", head, legacyChildMessage))
	f.git("update-ref", modern.Ref, legacyChild, modern.Commit)
	history, err := r.History(context.Background(), HistoryOptions{})
	if err != nil || len(history) != 3 {
		t.Fatalf("mixed history = %+v, %v", history, err)
	}
	wantMessages := []string{legacyChildMessage, "modern subject\n\nmodern body\n", legacyMessage}
	for i, message := range wantMessages {
		if history[i].Message != message || history[i].Base != head || history[i].Branch != "main" {
			t.Fatalf("mixed message %d = %+v, want %q", i, history[i], message)
		}
		comparison, err := r.Inspect(context.Background(), history[i].Commit, CompareOptions{})
		if err != nil || comparison.From != history[i].Parent || comparison.To != history[i].Commit {
			t.Fatalf("mixed inspection = %+v, %v", comparison, err)
		}
	}
	want := []FileChange{{Path: "tracked.txt", Status: "M", Added: 1, Deleted: 1}}
	comparison, err := r.Inspect(context.Background(), modern.Commit, CompareOptions{})
	if err != nil || !reflect.DeepEqual(comparison.Files, want) {
		t.Fatalf("mixed parent comparison = %+v, %v", comparison, err)
	}
	index, headBefore, working := f.read(".git/index"), f.git("rev-parse", "HEAD"), f.read("tracked.txt")
	deleted, err := r.Delete(context.Background(), DeleteOptions{Ref: modern.Ref, ExpectedTip: legacyChild})
	if err != nil || !deleted.Deleted || !bytes.Equal(index, f.read(".git/index")) || !bytes.Equal(working, f.read("tracked.txt")) || f.git("rev-parse", "HEAD") != headBefore {
		t.Fatalf("mixed-version deletion = %+v, %v", deleted, err)
	}
	// Pure legacy refs remain deletable too, without migration or object rewriting.
	f.git("update-ref", modern.Ref, legacy)
	deleted, err = r.Delete(context.Background(), DeleteOptions{Ref: modern.Ref, ExpectedTip: legacy})
	if err != nil || !deleted.Deleted {
		t.Fatalf("legacy deletion = %+v, %v", deleted, err)
	}
}

func TestCheckpointV2StrictMetadataAndFraming(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head := f.git("rev-parse", "HEAD")
	metadata := fmt.Sprintf(`{"branch":"main","base":"%s","message_bytes":5}`, head)
	validBody := "human\n\ngo-git-wip:2 " + metadata + "\n"
	variants := []struct{ name, body string }{
		{"unknown-footer-version", strings.Replace(validBody, "go-git-wip:2", "go-git-wip:99", 1)},
		{"legacy-footer-version", strings.Replace(validBody, "go-git-wip:2", "go-git-wip:1", 1)},
		{"missing-final-newline", strings.TrimSuffix(validBody, "\n")},
		{"extra-final-newline", validBody + "\n"},
		{"missing-blank-separator", strings.Replace(validBody, "human\n\n", "human\n", 1)},
		{"extra-separator-newline", strings.Replace(validBody, "human\n\n", "human\n\n\n", 1)},
		{"short-byte-count", strings.Replace(validBody, `"message_bytes":5`, `"message_bytes":4`, 1)},
		{"long-byte-count", strings.Replace(validBody, `"message_bytes":5`, `"message_bytes":6`, 1)},
		{"negative-byte-count", strings.Replace(validBody, `"message_bytes":5`, `"message_bytes":-1`, 1)},
		{"null-byte-count", strings.Replace(validBody, `"message_bytes":5`, `"message_bytes":null`, 1)},
		{"string-byte-count", strings.Replace(validBody, `"message_bytes":5`, `"message_bytes":"5"`, 1)},
		{"fraction-byte-count", strings.Replace(validBody, `"message_bytes":5`, `"message_bytes":5.0`, 1)},
		{"exponent-byte-count", strings.Replace(validBody, `"message_bytes":5`, `"message_bytes":5e0`, 1)},
		{"overflow-byte-count", strings.Replace(validBody, `"message_bytes":5`, `"message_bytes":999999999999999999999999999`, 1)},
		{"missing-byte-count", strings.Replace(validBody, `,"message_bytes":5`, "", 1)},
		{"duplicate-byte-count", strings.Replace(validBody, `"message_bytes":5`, `"message_bytes":5,"message_bytes":5`, 1)},
		{"duplicate-branch", strings.Replace(validBody, `"branch":"main"`, `"branch":"main","branch":"main"`, 1)},
		{"unknown-field", strings.Replace(validBody, `"message_bytes":5`, `"message_bytes":5,"extra":true`, 1)},
		{"missing-branch", strings.Replace(validBody, `"branch":"main",`, "", 1)},
		{"empty-branch", strings.Replace(validBody, `"branch":"main"`, `"branch":""`, 1)},
		{"empty-base", strings.Replace(validBody, `"base":"`+head+`"`, `"base":""`, 1)},
		{"null-branch", strings.Replace(validBody, `"branch":"main"`, `"branch":null`, 1)},
		{"wrong-branch-type", strings.Replace(validBody, `"branch":"main"`, `"branch":1`, 1)},
		{"missing-base", strings.Replace(validBody, `,"base":"`+head+`"`, "", 1)},
		{"wrong-base-type", strings.Replace(validBody, `"base":"`+head+`"`, `"base":[]`, 1)},
		{"nonobject-metadata", "human\n\ngo-git-wip:2 []\n"},
		{"trailing-metadata-data", strings.TrimSuffix(validBody, "\n") + " {}\n"},
		{"multiline-json", strings.Replace(validBody, `,"base"`, ",\n\"base\"", 1)},
		{"wrong-branch", strings.Replace(validBody, `"branch":"main"`, `"branch":"other"`, 1)},
		{"invalid-branch", strings.Replace(validBody, `"branch":"main"`, `"branch":"bad branch"`, 1)},
		{"short-base", strings.ReplaceAll(validBody, head, "short")},
		{"unavailable-base", strings.ReplaceAll(validBody, head, strings.Repeat("a", len(head)))},
		{"wrong-stack-base", ""},
		{"unknown-leading-namespace", "go-git-wip:99 anything\n\n" + validBody},
		{"malformed-leading-v1-with-v2-footer", "go-git-wip:1 {}\n\n" + validBody},
		{"unescaped-leading-v2", "go-git-wip:2 " + metadata + "\n\n" + validBody},
		{"unnecessary-escape", "Checkpoint: " + validBody},
		{"nul-message", strings.Replace(validBody, "human", "hu\x00an", 1)},
	}
	otherBase := f.git("commit-tree", f.git("rev-parse", "HEAD^{tree}"), "-p", head, "-m", "other normal commit")
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			body := variant.body
			if variant.name == "wrong-stack-base" {
				body = strings.ReplaceAll(validBody, head, otherBase)
			}
			tip := commitWithRawMessage(t, f, head, body)
			f.git("update-ref", "refs/wip/main", tip)
			index := f.read(".git/index")
			_, err := r.Capture(context.Background(), CaptureOptions{Baseline: true})
			requireErrorKind(t, err, KindInvalidRef)
			_, err = r.History(context.Background(), HistoryOptions{})
			requireErrorKind(t, err, KindInvalidRef)
			_, err = r.Delete(context.Background(), DeleteOptions{Ref: "refs/wip/main", ExpectedTip: tip})
			requireErrorKind(t, err, KindInvalidRef)
			if f.git("rev-parse", "refs/wip/main") != tip || f.git("rev-parse", "HEAD") != head || !bytes.Equal(index, f.read(".git/index")) {
				t.Fatal("malformed checkpoint was overwritten/deleted or changed workspace")
			}
		})
	}
}

func TestCheckpointV2MetadataWhitespaceOrderAndZeroLength(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head := f.git("rev-parse", "HEAD")
	for _, message := range []string{"human", ""} {
		body := fmt.Sprintf("%s\n\ngo-git-wip:2 { \"message_bytes\" : %d, \"base\" : \"%s\", \"branch\" : \"main\" } \n", message, len(message), head)
		tip := commitWithRawMessage(t, f, head, body)
		f.git("update-ref", "refs/wip/main", tip)
		history, err := r.History(context.Background(), HistoryOptions{})
		if err != nil || len(history) != 1 || history[0].Message != message {
			t.Fatalf("valid metadata whitespace/order/length rejected: %+v, %v", history, err)
		}
	}
}
