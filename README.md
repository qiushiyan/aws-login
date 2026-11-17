# aws-login

Signs the AWS CLI in to an IAM Identity Center session. Every profile on one
`[sso-session]` shares its sign-in, so `aws-login` signs out and in only
when the profile you name has no credentials, or when you ask for a new
session with `--renew`. Signing in to a second account never signs the first
one out.

```bash
aws-login                     # planlab-prod, signed in only if it has no credentials
aws-login planlab-personal    # any profile in ~/.aws/config
aws-login --renew             # a new session with the full 8 hours
aws-login --status            # identity and time left; exit 1 when nothing works
```

`aws-login --help` is the full reference. `make install` puts the binary in
`~/.local/bin`, and twin builds it on every machine (`twin tools install
aws-login`). `docs/design.md` is for changing the tool: the session clocks,
why it signs out when it does, and the behaviour callers rely on.

## Signing in

- **Expect the passkey prompt after a sign-out.** The access portal is
  signed out too. A run that only asks "Allow access" means the sign-out did
  not reach AWS and the old session's end time still holds: check the
  network before trusting the new token's clock.
- **A failed sign-in leaves every profile without credentials**, since the
  old session was already ended. Exit 1 tells an agent to escalate to a
  person rather than retry.
- **Each machine signs in on its own.** Refresh tokens rotate, so two
  machines cannot share a token cache or a stamp.

## Machines

- **Laptop:** the browser flow opens Arc, which redirects to a listener on
  localhost. The Codex Desktop automation "Refresh planlab-prod AWS SSO"
  (`~/.codex/automations/refresh-planlab-prod-aws-sso/`, untracked) runs
  every 8 hours: it runs `aws-login --status`, and `aws-login --renew` when
  less than 2 hours remain, and drives 1Password and the "Allow access"
  button by computer use. It must pass `--renew`: plain `aws-login` does
  nothing while the session lives, and `aws sso login` refreshes the token
  on the old session, so either leaves the 8 hours running from the last
  real sign-in.
- **The office minis:** `aws-login` uses the device-code flow there, since
  the laptop's browser cannot reach their localhost. Over SSH, dotfiles sets
  `BROWSER=browser-clip`, which sends the URL to the laptop's clipboard;
  paste it in any browser and approve.

## Checking that a run started a new session

The access portal lists your active sessions with their start times (IAM
Identity Center user guide, "Viewing and ending your active session").
After a run that signed in, the list should show one session started at the
time `aws-login` printed; a sign-in that reused the session leaves the old
start time in place. `aws-login --status` then keeps the answer local.

## Console links

`sso_start_url` in `~/.aws/config` is the Identity Center issuer URL
(`https://identitycenter.amazonaws.com/ssoins-7223ebd098f80c49`). The CLI
accepts it, but a browser gets 404 there. The access portal is
`https://ssoins-7223ebd098f80c49.portal.us-east-1.app.aws/`, and a link that
signs in to an account and role and then opens a console page is

```
<portal>/#/console?account_id=<id>&role_name=<role>&destination=<url-encoded console URL>
```

Account ids and role names are the profiles' `sso_account_id` and
`sso_role_name`. The Health API (`aws health describe-events`) answers
`SubscriptionRequiredException` on these accounts, which have no Business or
Enterprise support, so open issues are read in the console.
