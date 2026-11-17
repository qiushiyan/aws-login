package signin

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testRun(t *testing.T, session string, now time.Time) (*Run, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	return &Run{Profile: "planlab-dev", SessionHours: 8, StateDir: t.TempDir(), Now: func() time.Time { return now },
		out: &out, session: session}, &out
}

// The stamp is shared by every profile on an sso-session, so a sign-in
// through one profile dates the session for all of them.
func TestStampKeyedBySession(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	r, _ := testRun(t, "planlab", now)
	if err := r.stamp(now); err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(r.stampPath()); got != "planlab" {
		t.Errorf("stamp file %q, want planlab", got)
	}
	data, _ := os.ReadFile(r.stampPath())
	if string(data) != "1800000000\n" {
		t.Errorf("stamp %q, want the Unix seconds and a newline", data)
	}
	legacy, _ := testRun(t, "", now)
	if got := filepath.Base(legacy.stampPath()); got != "planlab-dev" {
		t.Errorf("legacy profile's stamp file %q, want its own name", got)
	}
}

func TestSessionTimes(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	for _, c := range []struct {
		name  string
		after time.Duration
		want  string
	}{
		{"time left", 90 * time.Minute, "6h30m left"},
		{"past the limit", 9 * time.Hour, "past the 8-hour limit"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, out := testRun(t, "planlab", start.Add(c.after))
			if err := r.stamp(start); err != nil {
				t.Fatal(err)
			}
			r.sessionTimes()
			if !bytes.Contains(out.Bytes(), []byte(c.want)) {
				t.Errorf("got %q, want it to contain %q", out, c.want)
			}
		})
	}
	r, out := testRun(t, "planlab", start)
	r.sessionTimes()
	if !bytes.Contains(out.Bytes(), []byte("signed in:  unknown")) {
		t.Errorf("without a stamp got %q, want the sign-in reported unknown", out)
	}
}

func TestOnMini(t *testing.T) {
	for machine, want := range map[string]bool{"mini\n": true, "mini2": true, "mac\n": false, "": false} {
		home := t.TempDir()
		if machine != "" {
			os.MkdirAll(filepath.Join(home, ".config"), 0o755)
			os.WriteFile(filepath.Join(home, ".config", "machine"), []byte(machine), 0o644)
		}
		if got := onMini(home); got != want {
			t.Errorf("machine %q: onMini %v, want %v", machine, got, want)
		}
	}
}
