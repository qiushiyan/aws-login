# aws-login's design

## Three clocks, and which one a sign-in resets

- **Access token** (`~/.aws/sso/cache/<hash>.json`, `expiresAt`): one hour.
  The CLI refreshes it on its own with the cached `refreshToken`, which is
  why the file's `expiresAt` is always about an hour ahead of its mtime and
  says nothing about when the sign-in ends.
- **Identity Center session** (AWS calls it the user interactive session):
  8 hours by default, 15 minutes to 90 days at the admin's choice; planlab's
  is 8 hours and not ours to change. Refresh works only while it lives; when
  it ends, the next refresh fails and every profile on the `sso-session`
  needs a new sign-in. This is the clock that forces the daily sign-ins.
- **Role credentials** (`~/.aws/cli/cache/`, from the permission set):
  issued per profile, up to 12 hours, and they keep working after the
  session ends.

`aws sso login` alone does **not** restart the second clock while the
browser is still signed in to the access portal: "If you already have an
active session, the existing session is reused and expires when the
existing session expires." A login that only shows "Allow access" hands the
CLI a new token on the old session, and the 8 hours still run from the first
sign-in. `aws sso logout` ends the session on AWS's side, so the sign-in
after it is a real one, with the full duration. Sources, read 2026-09-28:
the AWS CLI user guide `cli-configure-sso-concepts` (the reuse rule) and the
SSO Portal API `Logout` reference (the server-side end).

## The decision it makes

A run asks one question first: does the named profile get credentials
(`aws sts get-caller-identity`)? If so, the session is alive and the run
reports it, signing nothing in or out. If not, or with `--renew`, it runs
`aws sso logout`, `aws sso login`, verifies, and stamps the sign-in time.

The sign-out precedes every sign-in, since it is what restarts the session
clock. Its cost is that every profile on the session has no credentials
between the two, and none at all if the sign-in fails. A profile that
already works is therefore left alone: the planlab profiles share one
session, and signing in to a second account must not sign the first one
out. A renewal is asked for by name, with `--renew`.

`internal/signin` owns that decision and the stamp
(`$XDG_STATE_HOME/aws-login/<sso-session>`, Unix seconds and a newline, one
per sso-session, since its profiles share one sign-in). `internal/awscli` is
the only code that runs `aws`. `cmd/aws-login` translates flags.

## Contracts

The laptop's Codex automation (`README.md` § Machines says what it runs)
reads the exit code and the `identity:` line. `TestCompat` pins stdout, stderr, the exit code,
every aws call in order and the stamp for each case.

The command line runs on clikit: how help renders, how an argv error reads
and which errors are usage errors belong to clikit. A change there is a
clikit release plus a `go.mod` bump, reviewed through the help pages
(`make golden`).

## Differences from the shell script

The tool replaced `~/dotfiles/scripts/.local/bin/aws-login` (last at
dotfiles 230e206d). `testdata/compat` was first recorded from that script;
every case kept its bytes except these:

- An error line reads `aws-login: error: ...` rather than `aws-login: ...`.
- A failed sign-in reports the exit status of `aws sso login`; the script
  always printed `exit 0`.
- An unknown profile lists the known ones on the error line, comma-separated,
  then the usage line; still exit 2, nothing signed out.
- Two profiles, or an unknown flag, get clikit's usage error; still exit 2.
- `--status` and `--renew` together are a usage error; the script ignored
  `--renew`.
- `--session-hours` is a flag as well as `AWS_LOGIN_SESSION_HOURS`.
- A stamp that cannot be written fails the run (exit 1) after the sign-in;
  the script printed the shell's error and exited 0.
