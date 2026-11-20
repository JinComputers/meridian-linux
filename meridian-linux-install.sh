#!/bin/sh
# Install the published Debian preview for the current desktop user.
set -eu

if [ "$(uname -s)" != Linux ] || ! command -v dpkg >/dev/null 2>&1 ||
   [ "$(dpkg --print-architecture)" != amd64 ] || ! command -v apt-get >/dev/null 2>&1 ||
   [ ! -d /run/systemd/system ]; then
    echo 'Meridian: нужен Linux x86_64 с apt и systemd.' >&2
    exit 1
fi
if [ ! -r /etc/os-release ]; then
    echo 'Meridian: не удалось определить дистрибутив Linux.' >&2
    exit 1
fi
. /etc/os-release
case "${ID:-}" in
    debian|ubuntu) ;;
    *) echo 'Meridian: эта сборка подготовлена для Debian и Ubuntu.' >&2; exit 1 ;;
esac
if [ ! -t 0 ]; then
    if [ -r /dev/tty ] && [ -w /dev/tty ]; then
        exec sh "$0" "$@" </dev/tty
    fi
    echo 'Meridian: для установки нужен терминал с доступом администратора.' >&2
    exit 1
fi
command -v sha256sum >/dev/null 2>&1 || {
    echo 'Meridian: не найдена программа sha256sum.' >&2
    exit 1
}
if [ "$(id -u)" -ne 0 ]; then
    command -v sudo >/dev/null 2>&1 || {
        echo 'Meridian: для установки нужен sudo.' >&2
        exit 1
    }
fi
tmpdir=$(mktemp -d /tmp/meridian-deb.XXXXXXXX)
cleanup() {
    rm -f -- "$tmpdir/meridian.deb"
    rmdir -- "$tmpdir" 2>/dev/null || :
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM
chmod 0755 "$tmpdir"
package="$tmpdir/meridian.deb"
url='https://github.com/JinComputers/meridian-linux/releases/download/v0.0.1-preview1/meridian-vpn_0.0.1.preview1-a529cfc_amd64.deb'
echo 'Meridian: скачиваю тестовый пакет Linux x86_64.'
if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 15 --max-time 300 -o "$package" "$url"
elif command -v wget >/dev/null 2>&1; then
    wget -q -T 30 -O "$package" "$url"
else
    echo 'Meridian: нужен curl или wget с поддержкой HTTPS.' >&2
    exit 1
fi
actual=$(sha256sum "$package" | cut -d ' ' -f 1)
[ "$actual" = ea5f089b5a64c9637e8c8c0a65a14772e16fe2d63e566128ab0adf352bacafb0 ] || {
    echo 'Meridian: контрольная сумма пакета не совпадает.' >&2
    exit 1
}
chmod 0644 "$package"
echo 'Meridian: устанавливаю пакет и зависимости.'
if [ "$(id -u)" -eq 0 ]; then
    apt-get install "$package"
else
    sudo apt-get install "$package"
fi
echo 'Meridian установлен. Запустите приложение из меню.'
