module github.com/JinComputers/meridian-desktop/ui/tray-prototype

go 1.27.1

require (
	github.com/JinComputers/meridian-desktop v0.0.0-00010101000000-000000000000
	github.com/wailsapp/wails/v3 v3.0.0-beta.25
	golang.org/x/sys v0.48.0
	meridian/engine v0.0.0-00010101000000-000000000000
)

// Пробник живёт в отдельном модуле (тот же код кот 6 собирает на
// Windows своим go.mod — общий интерфейс, решение владельца 24.09),
// но с этапа 2 ему нужен core/control.go и platform/darwin (сокет,
// Keychain) из основного модуля — тот сам не go-gettable, тот же
// приём relative replace, что уже используют meridian-cli/-helper.
replace github.com/JinComputers/meridian-desktop => ../..

// meridian-desktop/go.mod сам держит replace meridian/engine => ...,
// но replace-директивы НЕ транзитивны (действуют только у главного
// модуля сборки, не у зависимостей) — этому go.mod нужна своя копия
// той же строки, иначе platform/darwin (через meridian/engine) не
// соберётся здесь.
replace meridian/engine => ../../engine

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/pion/dtls/v3 v3.1.5 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/stun/v3 v3.0.1 // indirect
	github.com/pion/transport/v3 v3.0.8 // indirect
	github.com/pion/transport/v4 v4.0.2 // indirect
	github.com/pion/turn/v4 v4.1.4 // indirect
	github.com/wailsapp/go-webview2 v1.0.22 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/mobile v0.0.0-20260820023541-8e8303b9da6c // indirect
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20260522210424-ecfc5a8d5446 // indirect
	golang.zx2c4.com/wireguard/windows v1.1.1 // indirect
)
