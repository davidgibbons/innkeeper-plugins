# password-login

Logs in with one password. Every login is the subject `owner`, so set the
instance's `owner_subject` to `owner`.

The `password_hash` secret is a bcrypt hash. Make one with the plugin, which
reads the password from stdin:

```bash
read -rs PW && printf '%s\n' "$PW" | ./password-login hash; unset PW
```
