package main

import (
	"regexp"
	"sort"
	"strings"

	"github.com/JinComputers/meridian-desktop/core"
)

// conflictPrograms — запущенные программы, которые мешают подключению:
// обходчики DPI и чужие VPN/прокси (панель роутера делает то же для
// своего списка, сводка кота 2 от 07.10). Ключ — имя процесса в нижнем
// регистре без ".exe".
var conflictPrograms = map[string]string{
	"tpws":            "Zapret",
	"nfqws":           "Zapret",
	"nfqws2":          "Zapret",
	"winws":           "Zapret",
	"zapret":          "Zapret",
	"goodbyedpi":      "GoodbyeDPI",
	"ciadpi":          "ByeDPI",
	"byedpi":          "ByeDPI",
	"spoofdpi":        "SpoofDPI",
	"sing-box":        "sing-box",
	"xray":            "Xray",
	"v2ray":           "V2Ray",
	"v2rayn":          "v2rayN",
	"mihomo":          "Clash",
	"clash":           "Clash",
	"clash-verge":     "Clash Verge",
	"clash verge":     "Clash Verge",
	"hiddify":         "Hiddify",
	"happ":            "Happ",
	"nekoray":         "NekoRay",
	"nekobox":         "NekoBox",
	"openvpn":         "OpenVPN",
	"openvpn connect": "OpenVPN",
	"openvpn-gui":     "OpenVPN",
	"wireguard-go":    "WireGuard",
	"wireguard":       "WireGuard",
	"warp-svc":        "Cloudflare WARP",
	"warp-taskbar":    "Cloudflare WARP",
	"cloudflare warp": "Cloudflare WARP",
	"amneziavpn":      "AmneziaVPN",
	"amnezia":         "AmneziaVPN",
	"outline":         "Outline",
	"nordvpn":         "NordVPN",
	"protonvpn":       "ProtonVPN",
	"expressvpn":      "ExpressVPN",
	"surfshark":       "Surfshark",
}

const conflictMaxNames = 4

// keyWordRe — слово «ключ» (ключ, ключа, ключом…), а не кусок слова: в
// «подключиться» подстрока «ключ» есть, и проверка по подстроке гасила
// подсказку почти для любого текста о неудаче подключения.
var keyWordRe = regexp.MustCompile(`(?i)(^|[^\p{L}])ключ`)

// conflictingPrograms — человеческие имена найденных мешающих программ из
// списка имён процессов; порядок по алфавиту, без повторов.
func conflictingPrograms(procs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range procs {
		p = strings.ToLower(strings.TrimSpace(p))
		p = strings.TrimSuffix(p, ".exe")
		if p == "" || strings.HasPrefix(p, "meridian") {
			continue
		}
		name, ok := conflictPrograms[p]
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// runningProcessNames — подменяется в тестах; в бою — platformProcessNames
// (conflicts_darwin.go / conflicts_windows.go).
var runningProcessNames = platformProcessNames

// conflictHintApplies — подсказка уместна при неудаче подключения по
// причине «не дошли до сервера»; отказ по ключу, пустое сообщение и «нет
// сети» от посторонних программ не зависят.
func conflictHintApplies(ep core.ControlErrorPayload, text string) bool {
	if text == "" || text == noNetworkText {
		return false
	}
	if ep.Failure == "silent" || ep.Failure == "refused" {
		return false
	}
	return !strings.Contains(ep.Message, "FATAL_") && !keyWordRe.MatchString(text)
}

// withConflictHint — текст ошибки с припиской о мешающих программах, если
// они сейчас запущены.
func withConflictHint(text string, ep core.ControlErrorPayload) string {
	if !conflictHintApplies(ep, text) {
		return text
	}
	names := conflictingPrograms(runningProcessNames())
	if len(names) == 0 {
		return text
	}
	more := ""
	if len(names) > conflictMaxNames {
		names, more = names[:conflictMaxNames], " и другие"
	}
	return text + ". Запущено, что может мешать: " + strings.Join(names, ", ") + more +
		". Выключите на время подключения"
}
