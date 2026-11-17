package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qiushiyan/clikit/clikittest"
)

// The behaviour callers depend on, case by case: stdout, stderr, the exit
// code, every aws call in order, and the stamp left behind, pinned in
// testdata/compat. The laptop's Codex automation reads the exit code and the
// identity: line; a person reads the rest. AGENTS.md names the callers.
//
// Record the files from the shell script this tool replaced, or any other
// build that must keep its bytes:
//
//	AWSLOGIN_COMPAT_OLD=/abs/path/to/aws-login go test ./cmd/aws-login -run TestCompat
//
// Without the variable each case runs in process and must match its file
// after normalize. An intended difference is re-recorded from the tree with
// AWSLOGIN_COMPAT_UPDATE=1 and reviewed as a diff; docs/design.md lists
// the differences from the shell script.

// stubAWS answers like the real CLI for the subcommands aws-login uses. A
// marker file stands in for the live session: sso login creates it, sso
// logout removes it, and sts answers only while it exists.
const stubAWS = `#!/bin/sh
printf '%s\n' "$*" >> "$STUB_LOG"
case "$1 $2" in
  "configure list-profiles") printf 'default\nplanlab-dev\nplanlab-prod\nplanlab-legacy\n' ;;
  "configure get")
    case "$*" in *planlab-legacy*) exit 1 ;; *) echo planlab ;; esac ;;
  "sso logout") rm -f "$STUB_SESSION"; echo "Successfully signed out of all SSO profiles." ;;
  "sso login")
    [ "${STUB_LOGIN_RC:-0}" -eq 0 ] || { echo "login stub: failing" >&2; exit "$STUB_LOGIN_RC"; }
    echo "Successfully logged into Start URL: https://example.invalid/start"
    : > "$STUB_SESSION" ;;
  "sts get-caller-identity")
    [ "${STUB_STS_RC:-0}" -eq 0 ] || exit "$STUB_STS_RC"
    [ -e "$STUB_SESSION" ] || { echo "Token has expired and refresh failed" >&2; exit 255; }
    echo "arn:aws:sts::000000000000:assumed-role/Stub/user" ;;
  *) echo "stub aws: unexpected: $*" >&2; exit 99 ;;
esac
`

// world is a temporary HOME with the stub aws first on a PATH that holds no
// real aws.
type world struct {
	t                     *testing.T
	home, stub, log, sess string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	w := &world{t: t, home: filepath.Join(root, "home"), stub: filepath.Join(root, "bin"),
		log: filepath.Join(root, "calls"), sess: filepath.Join(root, "session")}
	for _, d := range []string{filepath.Join(w.home, ".config"), w.stub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(w.stub, "aws"), []byte(stubAWS), 0o755); err != nil {
		t.Fatal(err)
	}
	// The shell script runs under a modern bash; the system PATH below has
	// only macOS's 3.2 and no aws.
	if bash, err := exec.LookPath("bash"); err == nil {
		if err := os.Symlink(bash, filepath.Join(w.stub, "bash")); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", w.home)
	t.Setenv("PATH", w.stub+":/usr/bin:/bin")
	t.Setenv("STUB_LOG", w.log)
	t.Setenv("STUB_SESSION", w.sess)
	for _, v := range []string{"XDG_STATE_HOME", "AWS_LOGIN_SESSION_HOURS", "STUB_LOGIN_RC", "STUB_STS_RC"} {
		t.Setenv(v, "") // restored after the test
		os.Unsetenv(v)
	}
	return w
}

func (w *world) signedIn() {
	if err := os.WriteFile(w.sess, nil, 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) stampAgo(d time.Duration) {
	dir := filepath.Join(w.home, ".local", "state", "aws-login")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		w.t.Fatal(err)
	}
	at := strconv.FormatInt(time.Now().Add(-d).Unix(), 10) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "planlab"), []byte(at), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) machine(name string) {
	if err := os.WriteFile(filepath.Join(w.home, ".config", "machine"), []byte(name+"\n"), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

// run invokes the tool, from AWSLOGIN_COMPAT_OLD when set, and returns the
// transcript the case is pinned by.
func (w *world) run(args ...string) string {
	var stdout, stderr string
	var code int
	if old := os.Getenv("AWSLOGIN_COMPAT_OLD"); old != "" {
		var o, e bytes.Buffer
		cmd := exec.Command(old, args...)
		cmd.Stdout, cmd.Stderr = &o, &e
		err := cmd.Run()
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			code = ee.ExitCode()
		} else if err != nil {
			w.t.Fatal(err)
		}
		stdout, stderr = o.String(), e.String()
	} else {
		r := clikittest.Run(app, args...)
		stdout, stderr, code = r.Stdout, r.Stderr, r.Code
	}
	calls, _ := os.ReadFile(w.log)
	var stamps strings.Builder
	dir := filepath.Join(w.home, ".local", "state", "aws-login")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		fmt.Fprintf(&stamps, "%s: %s", e.Name(), data)
	}
	return normalize(fmt.Sprintf("$ aws-login %s\n--- stdout\n%s--- stderr\n%s--- exit %d\n--- aws calls\n%s--- stamps\n%s",
		strings.Join(args, " "), stdout, stderr, code, calls, stamps.String()))
}

var (
	clockRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2} [A-Z][A-Za-z0-9+-]*`)
	leftRe  = regexp.MustCompile(`\d+h\d+m left`)
	epochRe = regexp.MustCompile(`(?m)^(\S+): \d{10}$`)
)

// normalize replaces what depends on the moment of the run.
func normalize(s string) string {
	s = clockRe.ReplaceAllString(s, "<time>")
	s = leftRe.ReplaceAllString(s, "<h>h<m>m left")
	return epochRe.ReplaceAllString(s, "$1: <epoch>")
}

func TestCompat(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(w *world)
		args  []string
	}{
		{"signed-out", nil, nil},
		{"signed-out-profile", nil, []string{"planlab-dev"}},
		{"signed-out-legacy", nil, []string{"planlab-legacy"}},
		{"live-other-profile", func(w *world) { w.signedIn(); w.stampAgo(time.Hour) }, []string{"planlab-dev"}},
		{"live-no-stamp", func(w *world) { w.signedIn() }, nil},
		{"live-past-limit", func(w *world) { w.signedIn(); w.stampAgo(9 * time.Hour) }, nil},
		{"renew", func(w *world) { w.signedIn(); w.stampAgo(time.Hour) }, []string{"--renew"}},
		{"mini-device-code", func(w *world) { w.machine("mini2") }, nil},
		{"laptop-browser", func(w *world) { w.machine("mac") }, nil},
		{"flags", nil, []string{"planlab-dev", "--use-device-code", "--no-browser"}},
		{"session-hours", func(w *world) { t.Setenv("AWS_LOGIN_SESSION_HOURS", "12") }, nil},
		{"login-fails", func(w *world) { w.signedIn(); t.Setenv("STUB_LOGIN_RC", "3") }, []string{"--renew"}},
		{"verify-fails", func(w *world) { t.Setenv("STUB_STS_RC", "254") }, nil},
		{"status-none", nil, []string{"--status"}},
		{"status-stamp", func(w *world) { w.signedIn(); w.stampAgo(time.Hour) }, []string{"--status"}},
		{"status-past-limit", func(w *world) { w.signedIn(); w.stampAgo(9 * time.Hour) }, []string{"--status"}},
		{"status-no-stamp", func(w *world) { w.signedIn() }, []string{"planlab-dev", "--status"}},
		{"status-legacy", func(w *world) { w.signedIn() }, []string{"planlab-legacy", "--status"}},
		{"unknown-profile", nil, []string{"no-such-profile"}},
		{"two-profiles", nil, []string{"planlab-dev", "planlab-prod"}},
		{"unknown-option", nil, []string{"--bogus"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			if c.setup != nil {
				c.setup(w)
			}
			pin(t, c.name, w.run(c.args...))
		})
	}
}

func pin(t *testing.T, name, got string) {
	t.Helper()
	file := filepath.Join("testdata", "compat", name+".txt")
	if os.Getenv("AWSLOGIN_COMPAT_UPDATE") == "1" || os.Getenv("AWSLOGIN_COMPAT_OLD") != "" {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%s: %v (AWSLOGIN_COMPAT_UPDATE=1 writes it)", name, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s:\n--- got\n%s--- want\n%s", name, file, got, want)
	}
}
