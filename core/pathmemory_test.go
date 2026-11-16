package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPathMemoryRememberAndForget(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))

	if got := pm.Remembered("net-a"); got != "" {
		t.Fatalf("новая сеть: запомнено %q, хочу пусто", got)
	}

	pm.Remember("net-a", "F")
	if got := pm.Remembered("net-a"); got != "F" {
		t.Fatalf("Remembered = %q, хочу F", got)
	}

	pm.NoteRelayWon("net-a")
	pm.Forget("net-a")
	if got := pm.Remembered("net-a"); got != "" {
		t.Fatalf("после Forget: запомнено %q, хочу пусто", got)
	}
	if n := pm.RelayStreak("net-a"); n != 0 {
		t.Fatalf("Forget обязан снять и relayStreak, получил %d", n)
	}
}

func TestPathMemoryForgetRungKeepsStreak(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))

	pm.Remember("net-a", "F")
	pm.NoteRelayWon("net-a")
	pm.NoteRelayWon("net-a")

	pm.ForgetRung("net-a")

	if got := pm.Remembered("net-a"); got != "" {
		t.Fatalf("ForgetRung обязан снять ступень, получил %q", got)
	}
	if n := pm.RelayStreak("net-a"); n != 2 {
		t.Fatalf("ForgetRung НЕ должен трогать relayStreak, получил %d, хочу 2", n)
	}
}

func TestPathMemorySkipDirectThresholdAndProbe(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	const skipAfter, probeEvery = 5, 4

	for n := 0; n < skipAfter; n++ {
		if pm.SkipDirect("net-a", skipAfter, probeEvery) {
			t.Fatalf("на счёте %d пропуск не должен ещё включиться", n)
		}
		pm.NoteRelayWon("net-a")
	}
	// Счёт теперь == skipAfter (5): 5%4 != 0 — пропуск включён.
	if !pm.SkipDirect("net-a", skipAfter, probeEvery) {
		t.Fatal("на счёте == skipAfter пропуск обязан включиться")
	}
	if pm.ProbingDirect("net-a", skipAfter, probeEvery) {
		t.Fatal("на счёте 5 (не кратно probeEvery) пробного захода быть не должно")
	}

	pm.NoteRelayWon("net-a") // счёт 6
	pm.NoteRelayWon("net-a") // счёт 7
	pm.NoteRelayWon("net-a") // счёт 8 — кратно probeEvery(4)

	if pm.SkipDirect("net-a", skipAfter, probeEvery) {
		t.Fatal("на кратном probeEvery счёте это ДОЛЖЕН быть пробный заход, не пропуск")
	}
	if !pm.ProbingDirect("net-a", skipAfter, probeEvery) {
		t.Fatal("на счёте 8 обязан быть пробный заход")
	}

	pm.ClearRelayStreak("net-a")
	if pm.SkipDirect("net-a", skipAfter, probeEvery) {
		t.Fatal("после ClearRelayStreak пропуск обязан снова быть выключен")
	}
}

func TestPathMemoryStreamAndDirectMuteIndependent(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))

	pm.NoteStreamMute("net-a")
	pm.NoteDirectMute("net-a")

	if n := pm.StreamMuteStreak("net-a"); n != 1 {
		t.Fatalf("streamMuteStreak = %d, хочу 1", n)
	}
	if n := pm.DirectMuteStreak("net-a"); n != 1 {
		t.Fatalf("directMuteStreak = %d, хочу 1", n)
	}

	// SKIP_AFTER=1 у немоты прямого (см. MeridianVpnService.
	// DIRECT_MUTE_SKIP_AFTER) — одной немоты достаточно.
	if !pm.SkipDirectMute("net-a", 1, 4) {
		t.Fatal("directMuteStreak=1, skipAfter=1: пропуск обязан включиться")
	}

	pm.ClearStreamMute("net-a")
	if n := pm.StreamMuteStreak("net-a"); n != 0 {
		t.Fatalf("после ClearStreamMute streamMuteStreak = %d, хочу 0", n)
	}
	// Немота прямого — отдельный счётчик, ClearStreamMute его не трогает.
	if n := pm.DirectMuteStreak("net-a"); n != 1 {
		t.Fatalf("ClearStreamMute не должен был тронуть directMuteStreak, получил %d", n)
	}
}

func TestPathMemoryDirectMuteSurvivesForget(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))

	pm.Remember("net-a", "F")
	pm.NoteDirectMute("net-a")
	pm.Forget("net-a")

	if n := pm.DirectMuteStreak("net-a"); n != 1 {
		t.Fatalf("directMuteStreak обязан пережить Forget (как у Android), получил %d, хочу 1", n)
	}
}

func TestPathMemoryPersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "path-memory.json")

	pm1 := NewPathMemory(path)
	pm1.Remember("net-a", "F")
	pm1.NoteRelayWon("net-b")
	pm1.NoteStreamMute("net-b")

	pm2 := NewPathMemory(path)
	if got := pm2.Remembered("net-a"); got != "F" {
		t.Fatalf("после перезапуска: Remembered(net-a) = %q, хочу F", got)
	}
	if n := pm2.RelayStreak("net-b"); n != 1 {
		t.Fatalf("после перезапуска: RelayStreak(net-b) = %d, хочу 1", n)
	}
	if n := pm2.StreamMuteStreak("net-b"); n != 1 {
		t.Fatalf("после перезапуска: StreamMuteStreak(net-b) = %d, хочу 1", n)
	}
}

func TestPathMemoryMissingAndCorruptFileAreHarmless(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "path-memory.json")

	// Файла ещё нет — не паника, просто пусто.
	pm := NewPathMemory(path)
	if got := pm.Remembered("net-a"); got != "" {
		t.Fatalf("без файла: Remembered = %q, хочу пусто", got)
	}

	// Битый JSON — тоже не паника (диск подвёл, работаем с пустой
	// памятью, а не роняем подключение).
	if err := os.WriteFile(path, []byte("{не json"), 0o600); err != nil {
		t.Fatal(err)
	}
	pm2 := NewPathMemory(path)
	if got := pm2.Remembered("net-a"); got != "" {
		t.Fatalf("битый файл: Remembered = %q, хочу пусто", got)
	}
	// И запись поверх битого файла обязана всё равно сработать.
	pm2.Remember("net-a", "F")
	pm3 := NewPathMemory(path)
	if got := pm3.Remembered("net-a"); got != "F" {
		t.Fatalf("после записи поверх битого файла: Remembered = %q, хочу F", got)
	}
}

func TestPathMemoryNilPathIsMemoryOnly(t *testing.T) {
	pm := NewPathMemory("")
	pm.Remember("net-a", "F")
	if got := pm.Remembered("net-a"); got != "F" {
		t.Fatalf("пустой path — память всё равно обязана работать в рамках процесса, получил %q", got)
	}
}

// --- pathMemoryOutcome (склейка с Connect: core/session.go) ---

func readyInputs() pathMemoryInputs {
	return pathMemoryInputs{RelayEnabled: true, HashesLen: 1, ProbeEvery: 4, NodeHosts: []string{"h1", "h2"}}
}

func TestPathMemoryBeforeLadderNilOrNoKeyIsNoop(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))

	if out := pathMemoryBeforeLadder(nil, "net-a", readyInputs()); out.key != "" {
		t.Fatal("pm == nil обязан дать нулевой outcome")
	}
	if out := pathMemoryBeforeLadder(pm, "", readyInputs()); out.key != "" {
		t.Fatal("key == \"\" обязан дать нулевой outcome (платформа без NetworkKeyer)")
	}

	var ladder LadderOptions
	pathMemoryOutcome{}.applyToLadder(&ladder)
	if ladder.Known != "" || ladder.SkipDirect || ladder.SkipStreams {
		t.Fatalf("нулевой outcome не должен трогать LadderOptions, получил %+v", ladder)
	}
}

func TestPathMemoryBeforeLadderNotReadyIgnoresStreaks(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	for i := 0; i < 10; i++ {
		pm.NoteRelayWon("net-a")
	}

	// relayReady=false (HashesLen=0) — пропуск не включается, сколько бы
	// ни было проигрышей: без кредов на релей уйти было бы некуда.
	in := readyInputs()
	in.HashesLen = 0
	out := pathMemoryBeforeLadder(pm, "net-a", in)

	var ladder LadderOptions
	out.applyToLadder(&ladder)
	if ladder.SkipDirect {
		t.Fatal("relayReady=false: SkipDirect не должен включиться")
	}
}

func TestPathMemoryBeforeLadderRelayOnlyModeIgnoresStreaks(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	for i := 0; i < 10; i++ {
		pm.NoteRelayWon("net-a")
	}

	in := readyInputs()
	in.Mode = LadderModeRelayOnly
	out := pathMemoryBeforeLadder(pm, "net-a", in)

	var ladder LadderOptions
	out.applyToLadder(&ladder)
	if ladder.SkipDirect {
		t.Fatal("режим не AUTO: SkipDirect не должен включиться (BuildLadder сам решает по Mode)")
	}
}

func TestPathMemoryBeforeLadderKnownAndNoRaceSkip(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pm.Remember("net-a", "node-F")
	for i := 0; i < 10; i++ {
		pm.NoteRelayWon("net-a")
	}

	out := pathMemoryBeforeLadder(pm, "net-a", readyInputs())
	var ladder LadderOptions
	out.applyToLadder(&ladder)

	if ladder.Known != "node-F" {
		t.Errorf("Known = %q, хочу node-F", ladder.Known)
	}
	if ladder.SkipDirect {
		t.Error("пропуск по проигрышам релею выключен (03.10): прямой пробуется всегда")
	}
	if ladder.SkipStreams {
		t.Error("streamMuteStreak=0: SkipStreams не должен включиться")
	}
}

func nodes(hosts ...string) []ParamsNode {
	var out []ParamsNode
	for i, h := range hosts {
		out = append(out, ParamsNode{ID: fmt.Sprintf("n%d", i), Host: h, Port: 1})
	}
	return out
}

func TestMuteAvoidSkipsOnlyMutedHost(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pm.NoteMuteHost("net-a", "h1")

	out := pathMemoryBeforeLadder(pm, "net-a", readyInputs())
	got := out.filterNodes(nodes("h1", "h2"))
	if len(got) != 1 || got[0].Host != "h2" {
		t.Fatalf("обходим только онемевший h1, получил %+v", got)
	}
	var ladder LadderOptions
	out.applyToLadder(&ladder)
	if ladder.SkipDirect {
		t.Fatal("онемел один вход из двух: мимо прямого целиком нельзя")
	}
}

func TestMuteAvoidAllMutedNeedsRelay(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pm.NoteMuteHost("net-a", "h1")
	pm.NoteMuteHost("net-a", "h2")

	out := pathMemoryBeforeLadder(pm, "net-a", readyInputs())
	if got := out.filterNodes(nodes("h1", "h2")); len(got) != 0 {
		t.Fatalf("онемели все входы, релей готов: прямых не остаётся, получил %+v", got)
	}

	in := readyInputs()
	in.HashesLen = 0
	out = pathMemoryBeforeLadder(pm, "net-a", in)
	if got := out.filterNodes(nodes("h1", "h2")); len(got) != 2 {
		t.Fatalf("без релея обходить нечем: все входы остаются, получил %+v", got)
	}
}

func TestMuteAvoidProbesEveryN(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pm.NoteMuteHost("net-a", "h1")
	in := readyInputs() // ProbeEvery 4
	probes := 0
	for i := 1; i <= 8; i++ {
		if len(pathMemoryBeforeLadder(pm, "net-a", in).avoidHosts) == 0 {
			probes++
		}
	}
	if probes != 2 {
		t.Fatalf("за 8 подключений при пробе раз в 4 хочу 2 пробы, получил %d", probes)
	}
}

func TestMuteAvoidRelayOnlyIgnored(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pm.NoteMuteHost("net-a", "h1")
	in := readyInputs()
	in.Mode = LadderModeRelayOnly
	if out := pathMemoryBeforeLadder(pm, "net-a", in); len(out.avoidHosts) != 0 {
		t.Fatal("в режиме только релея входы не участвуют")
	}
}

func TestPathMemoryOnLadderFailedClearsMuteHosts(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pm.Remember("net-a", "node-F")
	pm.NoteMuteHost("net-a", "h1")

	out := pathMemoryBeforeLadder(pm, "net-a", readyInputs())
	if len(out.avoidHosts) == 0 {
		t.Fatal("предпосылка: h1 обходится")
	}
	out.onLadderFailed(pm)

	if got := pm.Remembered("net-a"); got != "" {
		t.Errorf("отказ лестницы обязан забыть запомненную ступень, получил %q", got)
	}
	if got := pm.MuteHosts("net-a"); len(got) != 0 {
		t.Errorf("обход не помог — пометки входов снимаются, осталось %v", got)
	}
}

func TestNoteSessionEnd(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pm.Remember("net-a", "n0")
	s := &Session{pm: pm, pmOutcome: pathMemoryOutcome{key: "net-a"}, winner: "A", winnerHost: "h1"}

	s.noteSessionEnd(false, time.Minute)
	if len(pm.MuteHosts("net-a")) != 0 {
		t.Fatal("штатное отключение — не немота")
	}
	s.noteSessionEnd(true, 30*time.Second)
	if got := pm.MuteHosts("net-a"); len(got) != 1 || got[0] != "h1" {
		t.Fatalf("умерла сама за 30 с — вход онемел, получил %v", got)
	}
	if pm.Remembered("net-a") != "" {
		t.Fatal("онемевшая ступень не должна оставаться запомненной")
	}
	s.noteSessionEnd(true, muteWindow+time.Second)
	if len(pm.MuteHosts("net-a")) != 0 {
		t.Fatal("прожила дольше окна — пометка снимается")
	}

	relay := &Session{pm: pm, pmOutcome: pathMemoryOutcome{key: "net-a"}, winner: "R"}
	relay.noteSessionEnd(true, time.Second)
	if len(pm.MuteHosts("net-a")) != 0 {
		t.Fatal("релей не засчитывается входу")
	}
}

func TestNodeHost(t *testing.T) {
	info := &LadderInfo{IDs: map[string]string{"A": "n1"}}
	if got := nodeHost(nodes("h0", "h1"), info, "A"); got != "h1" {
		t.Fatalf("got %q", got)
	}
	if got := nodeHost(nodes("h0"), info, "R"); got != "" {
		t.Fatalf("релей: got %q", got)
	}
}

func TestPathMemoryOnWinnerUDPDirectRemembersAndClearsStreak(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	for i := 0; i < 3; i++ {
		pm.NoteRelayWon("net-a")
	}
	out := pathMemoryOutcome{key: "net-a"}
	info := &LadderInfo{IDs: map[string]string{"A": "node-A"}, Streams: map[string]bool{}}

	out.onWinner(pm, "A", info)

	if got := pm.Remembered("net-a"); got != "node-A" {
		t.Errorf("UDP-прямой обязан запомниться по устойчивому id, получил %q", got)
	}
	if n := pm.RelayStreak("net-a"); n != 0 {
		t.Errorf("прямой ожил — relayStreak обязан снятся в 0, получил %d", n)
	}
}

func TestPathMemoryOnWinnerStreamOrRelayDoesNotRememberAndForgetsRung(t *testing.T) {
	for _, winner := range []string{"R", "A"} { // "R" — релей; "A" помечена потоком ниже
		t.Run(winner, func(t *testing.T) {
			pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
			pm.Remember("net-a", "node-F")
			out := pathMemoryOutcome{key: "net-a", known: "node-F"}
			info := &LadderInfo{
				IDs:     map[string]string{"A": "node-A"},
				Streams: map[string]bool{"A": true},
			}

			out.onWinner(pm, winner, info)

			if got := pm.Remembered("net-a"); got != "" {
				t.Errorf("поток/релей не должны запоминаться, получил %q", got)
			}
			if n := pm.RelayStreak("net-a"); n != 1 {
				t.Errorf("relayStreak обязан вырасти, получил %d", n)
			}
		})
	}
}

func TestPathMemoryOnWinnerEmptyIsNoop(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pathMemoryOutcome{key: "net-a"}.onWinner(pm, "", nil)
	if n := pm.RelayStreak("net-a"); n != 0 {
		t.Fatal("winner==\"\" (движок не выдал буквы) не должен трогать память")
	}
}

// --- speedGuard (медленные пути) ---

func TestPathMemorySlowMarkSkipProbeClear(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	if pm.SkipSlow("net-a", 4) {
		t.Fatal("без пометки SkipSlow обязан быть false")
	}
	pm.MarkSlow("net-a", 80)
	if got := pm.SlowKbps("net-a"); got != 80 {
		t.Fatalf("SlowKbps = %d, хочу 80", got)
	}
	// probeEvery=4: попытки 1,2,3 — мимо прямого, 4-я — проба.
	want := []bool{true, true, true, false, true}
	for i, w := range want {
		if got := pm.SkipSlow("net-a", 4); got != w {
			t.Fatalf("попытка %d: SkipSlow = %v, хочу %v", i+1, got, w)
		}
	}
	pm.ClearSlow("net-a")
	if pm.SlowKbps("net-a") != 0 || pm.SkipSlow("net-a", 4) {
		t.Fatal("после ClearSlow пометки нет")
	}
	pm.MarkSlow("net-b", 0)
	if pm.SlowKbps("net-b") != 1 {
		t.Fatal("MarkSlow(0) обязан сохранить минимум 1 (0 — «нет пометки»)")
	}
}

// fakeClock подменяет часы памяти путей на время теста.
func fakeClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	old := pmNow
	pmNow = func() time.Time { return now }
	t.Cleanup(func() { pmNow = old })
	return &now
}

// Пометка медленного UDP не исключает потоковые ступени.
func TestPathMemoryBeforeLadderAppliesSlow(t *testing.T) {
	fakeClock(t)
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pm.MarkSlow("net-a", 90)
	out := pathMemoryBeforeLadder(pm, "net-a", readyInputs())
	var l LadderOptions
	out.applyToLadder(&l)
	if !out.skipSlow || !l.SkipDirect || l.SkipStreams {
		t.Fatalf("пометка стоит — лестница мимо прямого; outcome=%+v ladder=%+v", out, l)
	}
	// Без релея идти мимо прямого некуда.
	in := readyInputs()
	in.RelayEnabled = false
	if o := pathMemoryBeforeLadder(pm, "net-a", in); o.skipSlow {
		t.Fatal("релея нет — прямой не пропускаем")
	}
}

// Защита 1: пометку снимает только время (2 ч), а не замер релея.
func TestSlowMarkExpiresOnlyByTime(t *testing.T) {
	clk := fakeClock(t)
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pm.MarkSlow("net-a", 90)
	*clk = clk.Add(slowTTL - time.Minute)
	if !pm.SkipSlow("net-a", 100) {
		t.Fatal("до 2 ч пометка действует")
	}
	*clk = clk.Add(2 * time.Minute)
	if pm.SkipSlow("net-a", 100) || pm.SlowKbps("net-a") != 0 {
		t.Fatal("старше 2 ч пометка снимается сама")
	}
}

// Пометка без метки времени (файл до включения стража) — устарела.
func TestSlowMarkWithoutTimestampIsStale(t *testing.T) {
	fakeClock(t)
	path := filepath.Join(t.TempDir(), "path-memory.json")
	if err := os.WriteFile(path, []byte(`{"net-a":{"slow_kbps":90,"slow_n":3}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	pm := NewPathMemory(path)
	if pm.SkipSlow("net-a", 4) || pm.SlowKbps("net-a") != 0 {
		t.Fatal("старая пометка без времени не должна уводить с прямого")
	}
}

// Защита 2: смена пути по скорости — не чаще раза в 30 мин на сеть.
func TestSpeedSwitchGap(t *testing.T) {
	clk := fakeClock(t)
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	if !pm.SpeedSwitchAllowed("net-a") {
		t.Fatal("раньше не меняли — можно")
	}
	pm.NoteSpeedSwitch("net-a")
	if pm.SpeedSwitchAllowed("net-a") {
		t.Fatal("только что меняли — нельзя")
	}
	if !pm.SpeedSwitchAllowed("net-b") {
		t.Fatal("другая сеть независима")
	}
	*clk = clk.Add(speedSwitchGap - time.Second)
	if pm.SpeedSwitchAllowed("net-a") {
		t.Fatal("29:59 — ещё нельзя")
	}
	*clk = clk.Add(2 * time.Second)
	if !pm.SpeedSwitchAllowed("net-a") {
		t.Fatal("прошло 30 мин — можно")
	}
}

// onLadderFailed снимает пометку, которая сама завела пропуск прямого.
func TestPathMemoryOnLadderFailedClearsSlowItSkipped(t *testing.T) {
	fakeClock(t)
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	pm.MarkSlow("net-a", 90)
	out := pathMemoryBeforeLadder(pm, "net-a", readyInputs())
	out.onLadderFailed(pm)
	if pm.SlowKbps("net-a") != 0 {
		t.Fatal("лестница мимо прямого не поднялась — пометка снимается")
	}
}

func TestSpeedGuardDecision(t *testing.T) {
	cases := []struct {
		name       string
		onRelay    bool
		relayReady bool
		kbps       int64
		loss       int32
		want       speedAction
	}{
		{"быстрый прямой", false, true, 20000, 0, speedNone},
		{"медленный прямой", false, true, 90, 0, speedMarkSlow},
		{"ровно порог 1000", false, true, 1000, 0, speedNone},
		{"чуть ниже порога", false, true, 999, 0, speedMarkSlow},
		{"2 Мбит/с — раньше было медленно, теперь норма", false, true, 2000, 0, speedNone},
		{"потери 50%", false, true, 20000, 50, speedMarkSlow},
		{"потери 49%", false, true, 20000, 49, speedNone},
		{"медленный, но релея нет", false, false, 90, 0, speedNone},
		{"релей медленный — не судим", true, true, 100, 0, speedNone},
		{"релей с потерями — не судим", true, true, 100, 90, speedNone},
		{"релей быстрый", true, true, 83000, 0, speedNone},
	}
	for _, c := range cases {
		if got := speedGuardDecision(c.onRelay, c.relayReady, c.kbps, c.loss); got != c.want {
			t.Errorf("%s: %v, хочу %v", c.name, got, c.want)
		}
	}
}

func guardSession(t *testing.T, winner string, relayReady bool, state int32, kbps int64, loss int32) (*Session, *PathMemory, *int) {
	t.Helper()
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	stops := new(int)
	h := stubHandle{
		stop:       func() { *stops++ },
		probeState: state, probeKbps: kbps, probeLoss: loss,
	}
	return &Session{
		engine: h, pm: pm, winner: winner,
		pmOutcome: pathMemoryOutcome{key: "net-a", relayReady: relayReady},
	}, pm, stops
}

func TestRunSpeedGuardSlowDirectMarksAndStops(t *testing.T) {
	fakeClock(t)
	s, pm, stops := guardSession(t, "A", true, SpeedProbeDone, 90, 0)
	s.runSpeedGuard(context.Background())
	if pm.SlowKbps("net-a") != 90 || *stops != 1 || !s.speedSwitched.Load() {
		t.Fatalf("медленный прямой: SlowKbps=%d stops=%d, хочу 90 и 1", pm.SlowKbps("net-a"), *stops)
	}
	if pm.SpeedSwitchAllowed("net-a") {
		t.Fatal("смена по скорости обязана записаться")
	}
}

func TestRunSpeedGuardFastDoesNotStop(t *testing.T) {
	fakeClock(t)
	s, pm, stops := guardSession(t, "A", true, SpeedProbeDone, 50000, 0)
	s.runSpeedGuard(context.Background())
	if pm.SlowKbps("net-a") != 0 || *stops != 0 {
		t.Fatal("быстрый путь не трогаем")
	}
}

// Защита 1 на деле: на релее пометка остаётся при любом замере — и при
// «быстром», и при «медленном»; сессию не рвём.
func TestRunSpeedGuardRelayNeverClearsMark(t *testing.T) {
	fakeClock(t)
	for _, kbps := range []int64{100, 500, 83000} {
		s, pm, stops := guardSession(t, "R", true, SpeedProbeDone, kbps, 0)
		pm.MarkSlow("net-a", 90)
		s.runSpeedGuard(context.Background())
		if pm.SlowKbps("net-a") != 90 || *stops != 0 {
			t.Fatalf("релей %d кбит/с: пометка=%d stops=%d — пометку релей снимать не вправе, сессию рвать тоже", kbps, pm.SlowKbps("net-a"), *stops)
		}
	}
}

// Защита 2 на деле: второй медленный замер в пределах 30 мин сессию не рвёт
// и пометку не обновляет.
func TestRunSpeedGuardRespectsSwitchGap(t *testing.T) {
	clk := fakeClock(t)
	s, pm, stops := guardSession(t, "A", true, SpeedProbeDone, 90, 0)
	pm.NoteSpeedSwitch("net-a")
	*clk = clk.Add(10 * time.Minute)
	s.runSpeedGuard(context.Background())
	if *stops != 0 || pm.SlowKbps("net-a") != 0 || s.speedSwitched.Load() {
		t.Fatalf("путь меняли 10 мин назад — остаёмся: stops=%d slow=%d", *stops, pm.SlowKbps("net-a"))
	}
	*clk = clk.Add(25 * time.Minute)
	s.runSpeedGuard(context.Background())
	if *stops != 1 || pm.SlowKbps("net-a") != 90 {
		t.Fatal("через 35 мин смена снова разрешена")
	}
}

// Свод защит: петля 27.09 — медленный релей после подъёма. Цепочка подъёмов
// на одной сети за 3 часа не даёт больше одной смены в 30 мин и не оставляет
// пометку дольше 2 ч.
func TestNoSpeedLoopOverTime(t *testing.T) {
	clk := fakeClock(t)
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	switches := 0
	// Раз в 2 минуты новый подъём. Если пропуска нет — поднялись прямым и он
	// «медленный» (шейп); если пропуск есть — релей, замер которого занижен.
	for elapsed := time.Duration(0); elapsed < 3*time.Hour; elapsed += 2 * time.Minute {
		out := pathMemoryBeforeLadder(pm, "net-a", readyInputs())
		winner := "A"
		if out.skipSlow {
			winner = "R"
		}
		stops := 0
		s := &Session{
			engine: stubHandle{stop: func() { stops++ }, probeState: SpeedProbeDone, probeKbps: 80},
			pm:     pm, winner: winner,
			pmOutcome: pathMemoryOutcome{key: "net-a", relayReady: true},
		}
		s.runSpeedGuard(context.Background())
		switches += stops
		*clk = clk.Add(2 * time.Minute)
	}
	// 3 ч: смена в 0, 30, 60, 90, 120, 150 мин — не больше 6.
	if switches > 6 || switches == 0 {
		t.Fatalf("смен по скорости за 3 ч: %d, хочу от 1 до 6", switches)
	}
}

func TestRunSpeedGuardSilentAndCancelAndNoMemory(t *testing.T) {
	s, pm, stops := guardSession(t, "R", true, SpeedProbeSilent, 0, 100)
	s.runSpeedGuard(context.Background())
	if pm.SlowKbps("net-a") != 0 || *stops != 0 {
		t.Fatal("шлюз не ответил на релее — не судим")
	}
	s, pm, stops = guardSession(t, "A", true, SpeedProbeDone, 90, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.runSpeedGuard(ctx)
	if pm.SlowKbps("net-a") != 0 || *stops != 0 {
		t.Fatal("ctx отменён (человек остановил) — ничего не помечаем")
	}
	s, _, stops = guardSession(t, "A", true, SpeedProbeDone, 90, 0)
	s.pm = nil
	s.runSpeedGuard(context.Background())
	if *stops != 0 {
		t.Fatal("без памяти путей страж не работает")
	}
}

// Молчание замера: на прямом пути (релей есть) — путь режется, как медленный;
// на релее или без релея — не судим.
func TestRunSpeedGuardSilentDirectIsSlow(t *testing.T) {
	fakeClock(t)
	s, pm, stops := guardSession(t, "A", true, SpeedProbeSilent, 0, 100)
	s.runSpeedGuard(context.Background())
	if pm.SlowKbps("net-a") != 1 || *stops != 1 {
		t.Fatalf("молчание на прямом: SlowKbps=%d stops=%d, хочу 1 и 1", pm.SlowKbps("net-a"), *stops)
	}
	for _, c := range []struct {
		winner     string
		relayReady bool
	}{{"R", true}, {"A", false}} {
		s, pm, stops := guardSession(t, c.winner, c.relayReady, SpeedProbeSilent, 0, 100)
		s.runSpeedGuard(context.Background())
		if pm.SlowKbps("net-a") != 0 || *stops != 0 {
			t.Fatalf("молчание, winner=%s relayReady=%v — не судим", c.winner, c.relayReady)
		}
	}
	s, pm, stops = guardSession(t, "A", true, SpeedProbeSilent, 0, 100)
	pm.NoteSpeedSwitch("net-a")
	s.runSpeedGuard(context.Background())
	if *stops != 0 {
		t.Fatal("молчание в пределах 30 мин после смены — остаёмся")
	}
}

func TestRunSpeedGuardWaitsForRunningProbe(t *testing.T) {
	old, oldN := speedGuardPoll, speedGuardPolls
	speedGuardPoll, speedGuardPolls = time.Millisecond, 3
	defer func() { speedGuardPoll, speedGuardPolls = old, oldN }()
	s, pm, stops := guardSession(t, "A", true, SpeedProbeRunning, 0, 0)
	s.runSpeedGuard(context.Background())
	if pm.SlowKbps("net-a") != 0 || *stops != 0 {
		t.Fatal("замер не закончился за отведённое число опросов — ничего не делаем")
	}
}

// Уход по скорости не засчитывается входу как немота.
func TestNoteSessionEndSpeedSwitchIsNotMute(t *testing.T) {
	pm := NewPathMemory(filepath.Join(t.TempDir(), "path-memory.json"))
	s := &Session{pm: pm, winnerHost: "h1", pmOutcome: pathMemoryOutcome{key: "net-a"}}
	s.speedSwitched.Store(true)
	s.noteSessionEnd(true, 20*time.Second)
	if len(pm.MuteHosts("net-a")) != 0 {
		t.Fatal("сторож скорости остановил сессию — вход не онемел")
	}
	s2 := &Session{pm: pm, winnerHost: "h1", pmOutcome: pathMemoryOutcome{key: "net-a"}}
	s2.noteSessionEnd(true, 20*time.Second)
	if len(pm.MuteHosts("net-a")) != 1 {
		t.Fatal("настоящий обрыв через 20 с по-прежнему засчитывается как немота")
	}
}

func TestSlowUDPStillAttemptsTCPAndKeepsIt(t *testing.T) {
	fakeClock(t)
	s, pm, stops := guardSession(t, "A", true, SpeedProbeDone, 90, 0)
	pm.MarkSlow("net-a", 90)
	out := pathMemoryBeforeLadder(pm, "net-a", readyInputs())
	var ladder LadderOptions
	out.applyToLadder(&ladder)
	if !ladder.SkipDirect || ladder.SkipStreams {
		t.Fatalf("UDP должен пропускаться, TCP должен пробоваться: %+v", ladder)
	}
	s.winnerTCP = true
	s.runSpeedGuard(context.Background())
	if *stops != 0 || s.speedSwitched.Load() {
		t.Fatal("замер не должен разрывать выбранный TCP")
	}
}
