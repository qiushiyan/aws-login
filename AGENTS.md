# Working on aws-login

Signs the AWS CLI in to planlab's IAM Identity Center session, signing out
first only when the named profile has no credentials or `--renew` asks for a
new session. A person runs it at the terminal; the laptop's Codex automation
"Refresh planlab-prod AWS SSO" runs it every 8 hours. `docs/design.md` owns
the design; read it before changing behaviour.

- One maintainer's tool: commit on `main`, push at milestones, and keep
  changes proportional to demonstrated failures or requested features.
  Dependencies: the standard library, clikit and Kong.
- **Contracts other repositories depend on:** the exit codes, the
  `identity:` line, `--status`, `--renew` and the stamp file →
  the Codex automation's prompt on the laptop
  (`~/.codex/automations/refresh-planlab-prod-aws-sso/automation.toml`,
  untracked), described in `README.md` § Machines. Pinned by `TestCompat`
  (`cmd/aws-login/testdata/compat`). A change to one updates its consumers in
  the same pass.
- **A profile that already gets credentials is never signed out.** Profiles
  on one sso-session share its sign-in, and a sign-out ends it for all of
  them. Only a profile without credentials, or `--renew`, reaches
  `aws sso logout`. Held by `TestCompat` (`live-other-profile`, `renew`).
- **The sign-out precedes every sign-in.** `aws sso login` alone reuses a live
  session and its end time, so a renewal without it would not restart the
  clock. Held by `TestCompat`, whose pins list each aws call in order.
- **aws runs only through `internal/awscli`;** `internal/signin` owns when
  to sign out and the stamp's format.
- Tests never touch live state: the real `aws`, its token cache and
  `~/.local/state/aws-login`. Tests run a stub `aws` first on a PATH without
  the real one, under a temporary `HOME`.
- Ship: `make check`, then `make install`. Callers run `~/.local/bin/aws-login`
  and keep the old build until then; each other machine keeps its own until
  `twin sync aws-login`.
- The command line runs on clikit: how help renders and how argv errors read
  are clikit's (`~/dev/clikit`). Help, results and errors an agent reads
  follow `~/dotfiles/claude/.claude/skills/prompt-engineering/SKILL.md`;
  library questions go to `~/.agents/skills/find-docs/SKILL.md`.

Docs follow `~/dotfiles/docs/documentation-standards.md`, paths written from
the repository root.
