# aws-login's design

## The decision it makes

An Identity Center session is shared by every profile on its
`[sso-session]`, and it has a fixed length that the organisation sets
(8 hours). A run asks one question first: does the named profile get
credentials (`aws sts get-caller-identity`)? If so, the session is alive and
the run reports it, signing nothing in or out. If not, or with `--renew`, it
runs `aws sso logout`, `aws sso login`, verifies, and stamps the sign-in
time.

The sign-out comes before every sign-in because `aws sso login` alone reuses
a live session, and the new token inherits that session's end. The sign-out
ends the session on AWS's side, so the sign-in after it starts a full one.
The cost is that every profile on the session has no credentials between the
two, and none at all if the sign-in fails; that is why a profile that
already works is left alone.

`internal/signin` owns that decision and the stamp
(`$XDG_STATE_HOME/aws-login/<sso-session>`, Unix seconds and a newline, one
per sso-session, since its profiles share one sign-in). `internal/awscli` is
the only code that runs `aws`. `cmd/aws-login` translates flags.

## Contracts

The laptop's Codex automation runs `aws-login --status`, and
`aws-login --renew` when less than 2 hours remain, and reads the exit code
and the `identity:` line. `TestCompat` pins stdout, stderr, the exit code,
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
