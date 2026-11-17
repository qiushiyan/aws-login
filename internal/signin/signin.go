// Package signin decides when aws-login signs out and in, and keeps the
// stamp that records when a session began.
//
// Profiles that share an [sso-session] share one sign-in, so while that
// session lives, every profile on it can get credentials. A profile that
// already gets them is left alone; only a profile without credentials, or a
// renewal, goes through the sign-out and sign-in. The sign-out is what makes
// the sign-in start a new session with the full duration: aws sso login on its
// own reuses a session that is still alive, and the new token inherits that
// session's end (AWS CLI user guide, IAM Identity Center concepts).
package signin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/qiushiyan/aws-login/internal/awscli"
)

// Options is one run.
type Options struct {
	Profile    string
	Renew      bool // sign out and in even when the profile works
	DeviceCode bool // device-code flow; also chosen on the office minis
	NoBrowser  bool
	// SessionHours is the Identity Center session length; only the "ends"
	// estimate depends on it.
	SessionHours int
	Home         string // holds .config/machine
	StateDir     string // holds the stamps
	Now          func() time.Time
}

// Run is a profile resolved against the AWS config.
type Run struct {
	Options
	aws     awscli.CLI
	out     io.Writer
	session string // the profile's sso_session, "" for a legacy profile
}

// UnknownProfileError: the profile is not in the AWS config. Nothing was
// signed out.
type UnknownProfileError struct {
	Profile string
	Known   []string
}

func (e *UnknownProfileError) Error() string {
	return fmt.Sprintf("no profile '%s' in ~/.aws/config; known profiles: %s", e.Profile, strings.Join(e.Known, ", "))
}

// ErrNoCredentials: --status found the profile without working credentials,
// and has said so.
var ErrNoCredentials = errors.New("no working credentials")

// Open checks the profile against the AWS config before anything is signed
// out. out receives the report.
func Open(ctx context.Context, aws awscli.CLI, o Options, out io.Writer) (*Run, error) {
	known := aws.Profiles(ctx)
	if !slices.Contains(known, o.Profile) {
		return nil, &UnknownProfileError{Profile: o.Profile, Known: known}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Run{Options: o, aws: aws, out: out, session: aws.SSOSession(ctx, o.Profile)}, nil
}

// stampPath is keyed by the sso-session, which every profile on it shares;
// a legacy profile is its own key.
func (r *Run) stampPath() string {
	return filepath.Join(r.StateDir, cmp.Or(r.session, r.Profile))
}

// started reads the stamp: when aws-login last began this session.
func (r *Run) started() (time.Time, bool) {
	data, err := os.ReadFile(r.stampPath())
	if err != nil {
		return time.Time{}, false
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

func (r *Run) stamp(t time.Time) error {
	if err := os.MkdirAll(r.StateDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(r.stampPath(), []byte(strconv.FormatInt(t.Unix(), 10)+"\n"), 0o644)
}

func clock(t time.Time) string { return t.Local().Format("2006-01-02 15:04 MST") }

func (r *Run) printf(format string, a ...any) { fmt.Fprintf(r.out, format, a...) }

// sessionTimes prints when the session began and ends, from the stamp.
func (r *Run) sessionTimes() {
	started, ok := r.started()
	if !ok {
		r.printf("signed in:  unknown (last sign-in was not through aws-login)\n")
		return
	}
	ends := started.Add(time.Duration(r.SessionHours) * time.Hour)
	r.printf("signed in:  %s (by aws-login)\n", clock(started))
	left := int(ends.Sub(r.Now()) / time.Second)
	if left > 0 {
		r.printf("ends:       ~%s (%d-hour session, %dh%dm left)\n", clock(ends), r.SessionHours, left/3600, left%3600/60)
	} else {
		r.printf("ends:       ~%s — past the %d-hour limit; the role credentials still work until they expire, the next refresh will not\n", clock(ends), r.SessionHours)
	}
}

// Status reports the profile's credentials and session without signing
// anything in or out. It returns ErrNoCredentials, after saying so, when the
// profile has none.
func (r *Run) Status(ctx context.Context) error {
	r.printf("profile:    %s", r.Profile)
	if r.session != "" {
		r.printf(" (sso-session %s)", r.session)
	}
	r.printf("\n")
	arn := r.aws.Identity(ctx, r.Profile)
	if arn == "" {
		r.printf("credentials: none — run: aws-login %s\n", r.Profile)
		return ErrNoCredentials
	}
	r.printf("identity:   %s\n", arn)
	r.sessionTimes()
	return nil
}

// Login leaves a profile that already works signed in, and otherwise, or
// when renewing, signs out, signs in, verifies and stamps.
func (r *Run) Login(ctx context.Context) error {
	if !r.Renew {
		if arn := r.aws.Identity(ctx, r.Profile); arn != "" {
			r.printf("already signed in; nothing signed out")
			if r.session != "" {
				r.printf(" (every profile on sso-session %s shares this sign-in)", r.session)
			}
			r.printf("\nidentity:   %s\n", arn)
			r.sessionTimes()
			r.printf("run 'aws-login %s --renew' to sign out and start a new %d-hour session\n", r.Profile, r.SessionHours)
			return nil
		}
	}

	r.printf("==> aws sso logout (ends the current Identity Center session)\n")
	// A failed sign-out is not fatal: the sign-in still gives the profile
	// credentials, on the old session's clock (docs: the reuse case).
	_ = r.aws.Logout(ctx)

	args := awscli.LoginArgs(r.Profile, r.DeviceCode || onMini(r.Home), r.NoBrowser)
	r.printf("==> aws %s\n", strings.Join(args, " "))
	if err := r.aws.Login(ctx, args); err != nil {
		how := fmt.Sprintf("exit %d", awscli.ExitCode(err))
		if awscli.ExitCode(err) < 0 {
			how = err.Error()
		}
		return fmt.Errorf("login failed (%s); the old session is already ended, so '%s' has no credentials until a login succeeds", how, r.Profile)
	}
	started := r.Now()

	r.printf("==> aws sts get-caller-identity --profile %s\n", r.Profile)
	arn := r.aws.Identity(ctx, r.Profile)
	if arn == "" {
		return fmt.Errorf("login reported success but '%s' cannot get credentials; check the profile's account and role in ~/.aws/config", r.Profile)
	}

	if err := r.stamp(started); err != nil {
		return fmt.Errorf("signed in as %s, but the sign-in time was not recorded, so --status cannot say when the session ends: %w", arn, err)
	}
	r.printf("identity:   %s\n", arn)
	r.printf("signed in:  %s\n", clock(started))
	r.printf("ends:       ~%s (%d-hour session; run aws-login again after that)\n", clock(started.Add(time.Duration(r.SessionHours)*time.Hour)), r.SessionHours)
	return nil
}

// onMini reports whether this is one of the office minis, whose localhost
// listener the laptop's browser cannot reach, so the device-code flow is the
// default there. ~/.config/machine holds mac, mini or mini2.
func onMini(home string) bool {
	data, err := os.ReadFile(filepath.Join(home, ".config", "machine"))
	return err == nil && strings.HasPrefix(strings.TrimSpace(string(data)), "mini")
}
