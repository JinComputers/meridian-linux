module github.com/JinComputers/meridian-desktop

go 1.27.1

require (
	github.com/tailscale/wf v0.0.0-20240214030419-6fbb0a674ee6
	golang.org/x/net v0.58.0
	golang.org/x/sys v0.48.0
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2
	golang.zx2c4.com/wireguard v0.0.0-20260522210424-ecfc5a8d5446
	golang.zx2c4.com/wireguard/windows v1.1.1
	meridian/engine v0.0.0-00010101000000-000000000000
)

require (
	github.com/BurntSushi/toml v0.4.1 // indirect
	github.com/pion/dtls/v3 v3.1.5 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/stun/v3 v3.0.1 // indirect
	github.com/pion/transport/v3 v3.0.8 // indirect
	github.com/pion/transport/v4 v4.0.2 // indirect
	github.com/pion/turn/v4 v4.1.4 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	go4.org/netipx v0.0.0-20220725152314-7e7bdc8411bf // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20220218215828-6cf2b201936e // indirect
	golang.org/x/mobile v0.0.0-20260820023541-8e8303b9da6c // indirect
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	honnef.co/go/tools v0.3.2 // indirect
)

// meridian/engine (движок кота 3) не go-gettable: клонировать рядом с этим
// репозиторием (`gh repo clone JinComputers/meridian-android-src`),
// переключиться на тег engine-v1.10.12 (18.10.2026: ручная капча VK ждёт 5 минут, flood error_code=9 откладывает хеш на 30/60/120 мин; engine-v1.10.11: StartTrial; engine-v1.10.10, 16.10.2026: KeyAnswer.Tier и отказ FATAL_TIER_RELAY по ступени, без обрыва
// лестницы; engine-v1.10.9: замер скорости по хвосту пачки в 160 пакетов; engine-v1.10.8, 15.10.2026: QUIC UDP/443 в туннеле отклоняется сразу ICMP «порт недоступен» —
// проверено вживую на Windows; engine-v1.10.7: срок рукопожатия DTLS снимается сразу после него;
// engine-v1.10.6, 14.10.2026: отказ FATAL_BOUND_OTHER без автоповтора, молчание шлюза на Noise = FailSilent;
// engine-v1.10.5, 12.10.2026: соль обёртки MERIDIAN-WRAP-v1 вместо WDTT-WRAP-v1;
// engine-v1.10.4, 11.10.2026: тишина на прямом пути рвёт сессию за 45 с, на релее 90 с;
// engine-v1.10.2, 03.10.2026: выключен контрольный AUTH; включает
// ClampMSS/engine-v1.4.0, патч кота 6 на desktop.Start (сторож ctx до
// engine.Connect, не после — engine-v1.6.1), замер скорости туннеля
// (SetSpeedProbe/SpeedProbeState/Kbps/LossPct/RttMs, speedprobe.go —
// engine-v1.7.0; выключен в core, core/engine.go), engine/params без
// ключа в тексте ошибки (scrubErr — engine-v1.8.0), eventSink в
// engine/desktop — не паниковать при send на закрытый канал событий,
// когда OnStopped закрывает его раньше, чем растущие слоты дописали лог
// (находка кота 5 на Маке, engine-v1.8.1), и транспорт DION — второй
// источник TURN-кредов рядом с VK (engine.CheckDionRoom,
// Ladder.AddRelayDion — LadderOptions поля для DION нет, комнаты
// добавляются на готовую лестницу отдельно, core/session.go кота 5,
// engine-v1.9.0; тег и код — кот 3). Тег и
// коммит в meridian/engine для того, что касается desktop-пути (Mac/
// Windows), — с 26.09.2026 зона кота 6 (владелец); к коту 3 — за сверкой
// реализации/совместимости со шлюзом и остальными частями, не за тегом.
// См. platform/windows/docs/engine.md.
replace meridian/engine => ./engine
