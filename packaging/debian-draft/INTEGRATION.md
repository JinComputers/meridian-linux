# Debian integration — experimental

The launcher runs the desktop application as the ordinary user. If the helper
is unavailable or the control socket belongs to another UID, it requests one
administrator authorization with `pkexec` to run the fixed `meridian-setup`
program. The setup uses only the UID supplied by pkexec, requires exactly one
matching local account and refuses to replace a configured different UID. It
checks a root-owned configuration and rejects a leftover network journal.
It serializes concurrent setup calls, writes the configuration atomically with
mode 0600, then enables and starts the helper. It never handles a subscriber key.
If authorization or setup fails, the launcher shows one warning and still opens
the application. No application code runs as root.

Installation does not itself enable or start the service. The current service
unit intentionally does not pass the experimental networking flag; it cannot
establish a VPN connection. Do not distribute as a working package until the
helper's network operation, recovery and installation lifecycle have been
validated and the unit is configured consistently with the reviewed helper.

The setup selects one local UID; all processes of that UID can use the helper.
A second user is refused rather than silently replacing the authorization.
UID reuse, account removal and safe transfer to another account require further
work. The design does not claim support for shared machines or multiple sessions.
Manual edits of the configuration are an administrative operation and should be
performed only with the service stopped and network state clean.

Checks so far use a disposable Debian container with systemctl and binaries
replaced by test stand-ins: account setup, configuration ownership and mode,
refusal for a different UID, a residual journal, an unsafe configuration symlink,
concurrent setup attempts, refusal of authorization and successful-launch paths.
Real systemd, polkit dialogs, keyring, upgrade with active VPN, and real gateway
connection have not been tested. See `README.md` for package scaffold details.
