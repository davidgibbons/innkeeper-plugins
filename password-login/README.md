# password-login

Logs in with one password. Every login is the subject `owner`, so set the
instance's `owner_subject` to `owner`.

The usual way to set the password is the setup page on a new server: enter a
password of at least 12 characters and the plugin stores its bcrypt hash as
the `password_hash` secret.

To set the secret yourself, for example through
`INNKEEPER_OWNER_PASSWORD_HASH`, make a hash with the plugin, which reads the
password from stdin:

```bash
read -rs PW && printf '%s\n' "$PW" | ./password-login hash; unset PW
```
