# Meridian Linux: Debian packaging draft

This is an unreleased packaging scaffold, not a distributable package. It expects
separately built and verified binaries in `out/meridian-vpn` and
`out/meridian-helper`; no application source or secret is included. The current
Linux backend, frontend, installation lifecycle and network cleanup need further
validation before any package can be distributed.

The package does not enable or start the helper on install or upgrade. The graphical launcher asks for administrator authorization on first launch, and successful setup enables and starts the helper; see INTEGRATION.md. The unit
passes no `--enable-unverified-network` flag, so connecting remains disabled.
The helper takes a single non-root numeric UID in
`/etc/meridian/helper.env`; the shipped value is empty and the file is a Debian
configuration file. The authenticated launching user is enrolled on first setup; no account is created. Never put a
subscriber key in it. The unit keeps the runtime directory across a service stop.
The pre-removal hook stops the service and refuses removal or upgrade if its
network journal remains. Do not remove that journal to bypass the check: the
network may still need recovery. This only handles the systemd-managed helper,
not an independently launched helper or a helper whose cleanup failed without a
journal; these remain unresolved issues.

Debian `dh_installsystemd` supplies service lifecycle integration with
`--no-enable --no-start --no-restart-after-upgrade`. The package must be built
and its generated maintainer scripts inspected and tested on a disposable Debian
VM with systemd before use; the hook ordering and failures are not yet verified.
Actual desktop launch, secret service, GUI dependencies and icon are not validated.
The maintainer address matches the GitHub author configured in the source repository; no provenance, version, signature, architecture matrix or release method
has been chosen.

Open architectural decisions: how a user authorizes/configures the helper UID;
multi-user and seat switching policy; how to authenticate and persist the user's
key via Secret Service; service privilege/capability reduction; recoverability
and ownership of TUN, routes, DNS and nftables across crashes and power loss;
package upgrade/removal while connected or with unknown state; supported Debian
and Ubuntu versions, amd64/arm64, X11/Wayland, GUI dependencies; startup, update,
uninstall and rollback policies. Validate an isolated install, configure, enable,
connect, crash, recovery, upgrade, remove and purge before marking anything ready.

## Prototype package check (2026-11-16)

The scaffold was built on Debian 12 arm64 with debhelper using synthetic shell
stubs for both binaries and `dpkg-buildpackage -d` (the disposable builder did
not have the full build-essential metapackage). The test package is not a VPN
client. In a disposable Debian container, installation was forced despite missing
runtime dependencies: it did not enable the service; the root-owned config had
mode 0600; a synthetic journal blocked removal; after removing that synthetic
journal, remove preserved the conffile and purge removed it. Separate mock
systemctl tests confirmed stop was requested and a failed stop blocked removal.
During this check dh_fixperms was found to reset the intended file mode; the
packaging rule now sets mode 0600 after dh_fixperms. Generated postinst had no
service start command. The package was not tested with real systemd, installed
dependencies, actual executables, upgrades, or a live VPN. A VM with running
systemd remains required before any release.

An upgrade test with two synthetic stub packages exposed dpkg's fallback to the
new package's `prerm failed-upgrade` when the old `prerm upgrade` refuses. The
hook now checks that path too. In a disposable Debian 12 arm64 container with
mock systemctl and forced missing dependencies, an existing synthetic journal
blocked both scripts and kept the old version; after deleting only that test
journal, upgrade succeeded, preserved the edited UID and mode 0600, and did not
start or enable the unit. This did not test real systemd or real executables.
See `INTEGRATION.md` for the proposed source, build, UID and validation approach.

## Явное восстановление после сбоя

Если служба остановлена и обнаружен журнал сети, запуск приложения предлагает
восстановление. В окне указано, что восстановление снимет защитный фильтр и
вернёт прямой доступ к сети без VPN; действие выполняется только после согласия
и подтверждения администратора. При отказе приложение откроется без выполнения
восстановления. При ошибке журнал сохраняется, и настройки сети следует проверить.

Администратор также может запустить `sudo /usr/libexec/meridian/meridian-helper
--recover-network`: команда снимает оставшуюся защиту при успешной проверке
журнала и не возобновляет VPN. До её запуска убедитесь, что прямой доступ к сети
без VPN допустим. Не удаляйте вручную журнал или правила firewall как способ
обойти ошибку.
