package core

import (
	"log"
	"regexp"
	"strings"
)

// LadderDiagnostics включается только службой личной беты.
var LadderDiagnostics bool

// В журнал попадают только распознанные сообщения лестницы. Текст ошибок
// и ответы внешних служб исключены: в них могут встретиться секреты.
var ladderFailure = regexp.MustCompile(`^(?:ступень |потоковая ступень )?([A-Z][0-9]*)(?:: | — )(транспорт не поднялся|AUTH не прошёл|неудача за [^:]+): (.*)$`)

func ladderFailureReason(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "context canceled") || strings.Contains(s, "context cancelled"):
		return "попытка отменена"
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded") || strings.Contains(s, "тайм-аут") || strings.Contains(s, "не ответил"):
		return "нет ответа за отведённое время"
	case strings.Contains(s, "connection refused"):
		return "соединение отклонено"
	case strings.Contains(s, "network is unreachable") || strings.Contains(s, "no route to host"):
		return "сеть или маршрут недоступны"
	default:
		return "причина не распознана"
	}
}

var ladderEventPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^ступень ([A-Z][0-9]*) (запомнена|— пробую|— успех|— неудача)`),
	regexp.MustCompile(`^ступень ([A-Z][0-9]*): (AUTH не прошёл)`),
	regexp.MustCompile(`^потоковая ступень ([A-Z][0-9]*): (AUTH не прошёл)`),
	regexp.MustCompile(`^([A-Z][0-9]*): (транспорт готов ПЕРВЫМ|транспорт готов|опоздала, транспорт закрыт|без DTLS, рукопожатия нет|транспорт создан|рукопожатие прошло|отпечаток шлюза подтверждён|поток — проверяю одной меткой|запомненная ступень — проверяю одной меткой)`),
	regexp.MustCompile(`^([A-Z][0-9]*): (метка [0-9]+ — эхо вернулось за [0-9.]+(?:ms|s|µs)|эха на метку [0-9]+ нет за [0-9.]+(?:ms|s|µs)|шлюз без эха — проверить ступень нечем, беру как есть)`),
	regexp.MustCompile(`^(потоковые ступени наперегонки, круг [0-9]+|потоковые ступени, круг 1: гонка запущена заранее, жду итога)`),
	regexp.MustCompile(`^(гонка транспорта не дала победителя|потоковая гонка не дала победителя|поток поднялся раньше UDP — круг UDP прерван|лестница: общий срок исчерпан, потоковые ступени не пробовались|проверка подлинности шлюза \(Noise NK\) пройдена|AUTH отправлен, жду ответ|ответ на AUTH получен)`),
}

func logLadderEvent(ev EngineEvent) {
	if !LadderDiagnostics || ev.Kind != "log" {
		return
	}
	if m := ladderFailure.FindStringSubmatch(ev.Reason); m != nil {
		log.Printf("лестница: %s: %s — %s", m[1], m[2], ladderFailureReason(m[3]))
		return
	}
	for _, pattern := range ladderEventPatterns {
		if m := pattern.FindString(ev.Reason); m != "" {
			log.Printf("лестница: %s", strings.TrimSpace(m))
			return
		}
	}
}

// Start возвращает буфер даже при ошибке. Не ждём закрытия: некоторые
// ранние ошибки могут вернуть канал, который ещё не закрыт.
func logLadderEvents(events <-chan EngineEvent) {
	for events != nil {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			logLadderEvent(ev)
		default:
			return
		}
	}
}
