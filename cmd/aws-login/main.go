// aws-login signs the AWS CLI in to planlab's IAM Identity Center session,
// starting a new session only when the named profile has no credentials or
// a renewal is asked for.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"
	"github.com/qiushiyan/clikit"

	"github.com/qiushiyan/aws-login/internal/awscli"
	"github.com/qiushiyan/aws-login/internal/signin"
)

type CLI struct {
	Profile string `arg:"" optional:"" default:"planlab-prod" help:"Profile from ~/.aws/config to sign in, or to report with --status."`

	Status bool `xor:"mode" help:"Report the profile's identity and when its session ends, without signing anything in or out. Exits 1 when the profile has no working credentials."`
	Renew  bool `xor:"mode" help:"Sign out and start a new session even when the profile already works, to restart the session's clock. Every profile on the same sso-session loses its credentials until the sign-in succeeds."`

	UseDeviceCode bool `help:"Sign in with a code entered in any browser instead of a browser redirect to this machine. Always on for the office minis, whose localhost the laptop's browser cannot reach."`
	NoBrowser     bool `help:"Print the sign-in URL instead of opening a browser."`
	SessionHours  int  `default:"8" env:"AWS_LOGIN_SESSION_HOURS" help:"Length of an Identity Center session, set by the organisation. Only the reported end time depends on it."`
}

func (CLI) Help() string {
	return `
All planlab profiles share one Identity Center session (the [sso-session
planlab] block in ~/.aws/config), so while that session lives, every
profile on it can get credentials, and signing in to one profile never
needs to sign another out.

aws-login first asks whether the profile already gets credentials. If it
does, nothing is signed in or out: it prints the identity and when the
session ends. If it does not, or with --renew, it runs aws sso logout, which
ends the session on AWS's side and clears every profile's cached
credentials, then aws sso login, then verifies with aws sts
get-caller-identity and records the sign-in time. The sign-out is what
gives the new session its full length: aws sso login alone reuses a
session that is still alive, along with its end time. Expect the full
sign-in, passkey included, after a sign-out; a sign-in that only asks to
"Allow access" means the sign-out did not reach AWS and the old end time
still holds.

A session ends 8 hours after its sign-in, and the organisation sets that,
not this tool. The role credentials a profile already holds keep working
until they expire; the next refresh after the session ends fails, and the
profile needs aws-login again.

A failed sign-in leaves every profile on the session without credentials,
since the old session was already ended. Exit 1 then means a person has to
sign in; retrying without one only repeats the prompt.

Each line of the report starts with a label (profile:, identity:, signed in:,
ends:, credentials:); a run that signed in or found the profile working
prints an identity: line.`
}

func (CLI) HelpSections() []clikit.Section {
	return []clikit.Section{
		{Title: "Files", Body: `
~/.local/state/aws-login/<sso-session>
    The sign-in time aws-login recorded, in Unix seconds, one file per
    sso-session (per profile for a profile without one); under
    $XDG_STATE_HOME when that is set. --status reads it; a sign-in made with
    aws sso login directly is reported as unknown.
~/.config/machine
    mac, mini or mini2. A value starting with mini turns on
    --use-device-code.`},
		{Title: "Examples", Body: `
aws-login                      planlab-prod, signing in only if it has no credentials
aws-login planlab-personal     another account on the same session; nothing signed out
aws-login --renew              a new 8-hour session, whatever the current one has left
aws-login --status             identity and time left; exit 1 when nothing works`},
	}
}

func (c *CLI) Run(ctx *kong.Context, stdin io.Reader) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	bg := context.Background()
	aws := awscli.CLI{Stdin: stdin, Stdout: ctx.Stdout, Stderr: ctx.Stderr}
	run, err := signin.Open(bg, aws, signin.Options{
		Profile:      c.Profile,
		Renew:        c.Renew,
		DeviceCode:   c.UseDeviceCode,
		NoBrowser:    c.NoBrowser,
		SessionHours: c.SessionHours,
		Home:         home,
		StateDir:     filepath.Join(state, "aws-login"),
	}, ctx.Stdout)
	if err != nil {
		return err
	}
	if c.Status {
		return run.Status(bg)
	}
	return run.Login(bg)
}

var app = clikit.App{
	Name:        "aws-login",
	Description: "aws-login signs the AWS CLI in to an IAM Identity Center session, signing out first only when a new session is needed.",
	New:         func() any { return &CLI{} },
	ExitCodes: map[int]string{
		1: "the sign-in or its verification failed, or --status found no working credentials",
		2: "bad arguments, or a profile not in ~/.aws/config; nothing was signed out",
	},
	Outcome: func(err error) error {
		if _, ok := errors.AsType[*signin.UnknownProfileError](err); ok {
			return clikit.Usage(err)
		}
		if errors.Is(err, signin.ErrNoCredentials) {
			return clikit.Exit(1)
		}
		return err
	},
}

func main() { os.Exit(app.Run(os.Args[1:])) }
