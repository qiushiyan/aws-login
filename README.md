# aws-login

Signs the AWS CLI in to an IAM Identity Center session. Every profile on one
`[sso-session]` shares its sign-in, so `aws-login` signs out and in only
when the profile you name has no credentials, or when you ask for a new
session with `--renew`. Signing in to a second account never signs the first
one out.

```bash
aws-login                     # planlab-prod, signing in only if it has no credentials
aws-login planlab-personal    # another profile; nothing signed out while the session lives
aws-login --renew             # sign out and start a new session with its full length
aws-login --status            # identity and time left; exit 1 when nothing works
```

`aws-login --help` is the full reference. Install with `make install`, which
puts the binary in `~/.local/bin`.

The session clocks, the console links and how each machine signs in are in
`~/dotfiles/docs/aws-sso.md`; `docs/design.md` here is for changing the tool.
