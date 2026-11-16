# Integration plan — not a release procedure

1. Assemble an isolated source snapshot, including the desktop core and exactly
   pinned engine source, after review of license, provenance and secrets. Replace
   the local absolute/relative engine reference with a reproducible, pinned source
   layout; record both revisions. Never silently publish the adjacent Android
   checkout or copy private test keys.
2. Build the Linux Wails GUI as `out/meridian-vpn` and helper as
   `out/meridian-helper` for the same target architecture. The GUI prototype has
   compiled on Debian 12 arm64 with `-tags gtk3`; dependency checks identified
   GTK 3, WebKitGTK 4.1, libsoup 3 and JavaScriptCoreGTK 4.1. Build and inspect
   dynamic library requirements in each target distribution; use `${shlibs:Depends}`
   and verify its generated values with the real executable. `libsecret-tools` and
   a functioning user-session Secret Service are separately required. Debian amd64,
   Ubuntu variants, X11, Wayland and the icon still require verification.
3. Feed those exact binaries into the `debian/` scaffold and inspect the built
   package: contents, hashes, permissions, conffile ownership, dependency metadata,
   scripts and service unit. Use a clean Debian builder and document the Go compiler,
   toolchain and dependency hashes. Compare the package binaries to tested builds.
4. The installed unit must remain disabled and unstarted by default, and must not
   pass `--enable-unverified-network`. Confirm no install, upgrade, login, desktop
   entry or first run starts it. In a disposable systemd VM test service security,
   normal and failed start/stop, removal/upgrade while connected, crash recovery,
   journal preservation, force-quit, socket stale state and restoration of DNS,
   IPv4, IPv6 and firewall before changing this policy.
5. Only after the network implementation and all failure paths are verified, test
   real connectivity and the complete install/connect/disconnect/reboot/remove
   lifecycle in isolated Linux VMs, then document untested configurations. No release
   or public distribution is authorized by this packaging plan.

## UID selection and trust boundary

For the experimental package, the administrator must select one existing non-root
local account explicitly. A root-owned `/etc/meridian/helper.env` contains only its
numeric `MERIDIAN_UID`; it is not a credential store. No account or UID is inferred
from installation, `SUDO_USER`, filesystem ownership, environment variables,
active console heuristics, or a UI-supplied value. The initial value is empty, so
starting the unit fails closed until explicitly configured. Package installation
and merely choosing a UID do not start or enable the service.

The helper verifies the peer's UID from kernel `SO_PEERCRED`; the UI independently
verifies that the connected helper UID is root before sending a key. This authorizes
all processes of that local UID and does not establish executable identity or
per-seat isolation. A numeric UID can be reassigned later; account deletion,
renaming or reassignment must invalidate/revalidate configuration before such a
scheme can be called production-ready. There is no automatic multi-user or seat
switching; a different UID requires an administrator-controlled reconfiguration
while the old service is stopped and the network state is clean.

For a future GUI setup, assess a narrowly scoped polkit-authorized administrative
operation that validates the caller and intended local identity against logind,
atomically writes a root-owned configuration with symlink-safe file operations,
checks no session or residual journal exists, and requires an explicit user action
to start the service. The polkit policy, exact semantics and stale-UID handling
are not designed or implemented; do not ship a generic root command or treat this
proposal as an authorization mechanism. Security-review and adversarial tests are
required, including session changes, reinstallation and malicious peer scenarios.
