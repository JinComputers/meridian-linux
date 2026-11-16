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

A later Debian 12 ARM64 systemd container exercised real systemd, D-Bus,
polkit and pkexec together with the real ARM64 helper and a GUI stand-in.
The caller UID was installed and the helper socket appeared with owner UID
and mode 0600; start/stop and the dpkg removal guard with a synthetic journal
passed. A test-only polkit rule granted the single setup action without an
interactive administrator prompt. A real AMD64 helper also started/stopped
through an explicit qemu-x86_64 override under systemd, with network disabled.
These checks do not prove actual graphical authorization, a working tunnel,
native x86_64 operation or the completed package lifecycle.

## Explicit network recovery

If a journal remains while the service is inactive, the launcher warns that
recovery removes the fail-closed firewall and restores direct network access
without a VPN. Recovery runs only after explicit confirmation and administrator
authorization. `meridian-recover` validates the configured UID and calls the
helper recovery operation under its own lock before starting the service. If
recovery fails, it must retain the journal and the launcher reports failure.
The service's unverified-network flag remains disabled pending network testing.
