package core

// appsplit.go — раздельное туннелирование по приложениям (решение владельца
// 27.10.2026): в чёрном режиме (core.TunnelRouter) сверх списка адресов и
// доменов в туннель идёт трафик выбранных человеком приложений. Всё
// остальное по-прежнему напрямую.
//
// Приложение задаётся путём: полный путь к exe (один процесс) или каталог с
// обратной косой чертой на конце (все exe внутри, включая подкаталоги —
// установленная программа с помощниками). Регистр не важен. Разбор и
// сравнение — platform/windows/appsplit (Windows-only; на Mac — Кот 5).

// AppSplitRouter — НЕОБЯЗАТЕЛЬНЫЙ интерфейс платформы (как AntiDPIRouter):
// платформа без него при непустом списке приложений → Connect отказывает
// вслух. Действует ТОЛЬКО в чёрном режиме (TunnelOnly).
type AppSplitRouter interface {
	// ApplyAppSplit — поднять перехват после того, как туннель и физический
	// путь уже известны (зовётся ПОСЛЕ apply/ApplyTunnelOnly). Ошибка не
	// обрывает подключение: туннель по списку работает и без приложений —
	// лучше, чем ничего (тот же принцип, что у AntiDPI).
	ApplyAppSplit(st *State, t Tun, info TunnelInfo, apps []string) error
	// StopAppSplit — снять перехват; идемпотентно (Session.Disconnect зовёт
	// безусловно, если ApplyAppSplit встал).
	StopAppSplit()
}

// NormalizeApps — список приложений из настроек: без пустых строк и
// повторов (без учёта регистра), порядок сохранён.
func NormalizeApps(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = trimSpace(s)
		if s == "" {
			continue
		}
		k := lowerASCII(s)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n' || s[i] == '"') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r' || s[j-1] == '\n' || s[j-1] == '"') {
		j--
	}
	return s[i:j]
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
