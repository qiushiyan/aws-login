// Package awscli runs the aws command for aws-login. Queries keep their
// stderr, as the shell script's 2>/dev/null did; the sign-out and sign-in
// write to the caller's terminal, where the person signing in reads the
// device code or the browser prompt.
package awscli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
)

// CLI runs aws from PATH. Stdin, Stdout and Stderr are what the sign-out and
// sign-in inherit.
type CLI struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// query runs aws and returns its trimmed stdout; stderr is discarded.
func (CLI) query(ctx context.Context, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "aws", args...)
	cmd.Stdout = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// Profiles lists the profiles in the AWS config. A failed listing is an
// empty list, which no profile name matches.
func (c CLI) Profiles(ctx context.Context) []string {
	out, err := c.query(ctx, "configure", "list-profiles")
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// SSOSession is the profile's sso_session, or "" for a profile without one.
func (c CLI) SSOSession(ctx context.Context, profile string) string {
	out, err := c.query(ctx, "configure", "get", "sso_session", "--profile", profile)
	if err != nil {
		return ""
	}
	return out
}

// Identity is the ARN the profile's credentials resolve to, or "" when the
// profile cannot get credentials.
func (c CLI) Identity(ctx context.Context, profile string) string {
	out, err := c.query(ctx, "sts", "get-caller-identity", "--profile", profile, "--output", "text", "--query", "Arn")
	if err != nil {
		return ""
	}
	return out
}

func (c CLI) passthrough(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "aws", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Stdin, c.Stdout, c.Stderr
	return cmd.Run()
}

// Logout runs aws sso logout, which ends the Identity Center session on
// AWS's side and clears every profile's cached credentials.
func (c CLI) Logout(ctx context.Context) error { return c.passthrough(ctx, "sso", "logout") }

// LoginArgs are the arguments of aws sso login for profile.
func LoginArgs(profile string, deviceCode, noBrowser bool) []string {
	args := []string{"sso", "login", "--profile", profile}
	if deviceCode {
		args = append(args, "--use-device-code")
	}
	if noBrowser {
		args = append(args, "--no-browser")
	}
	return args
}

// Login runs aws sso login with args from LoginArgs.
func (c CLI) Login(ctx context.Context, args []string) error { return c.passthrough(ctx, args...) }

// ExitCode is the status a failed command exited with, or -1 when it did not
// run or was killed.
func ExitCode(err error) int {
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return ee.ExitCode()
	}
	return -1
}
