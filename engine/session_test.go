package engine

import (
	"strings"
	"testing"
)

// Привязка к другому устройству — отдельная причина, не общий отказ по
// паролю, и обрывает лестницу (кот 5, 14.10).
func TestClassifyBoundOther(t *testing.T) {
	if got := classifyAuthRefusal("FATAL_BOUND_OTHER"); got != errBoundOther {
		t.Fatalf("FATAL_BOUND_OTHER → %v, ждали errBoundOther", got)
	}
	// С подробностями после маркера — тоже по вхождению.
	if got := classifyAuthRefusal("FATAL_BOUND_OTHER device=xxxx"); got != errBoundOther {
		t.Fatalf("маркер с хвостом не распознан: %v", got)
	}
	if !fatalForLadder(errBoundOther) {
		t.Fatal("errBoundOther должен обрывать лестницу")
	}
	if !strings.Contains(errBoundOther.Error(), "FATAL_BOUND_OTHER") {
		t.Fatal("в тексте нет маркера FATAL_BOUND_OTHER — клиент не выберет текст")
	}
	// Не спутать с обычным отказом по паролю.
	if got := classifyAuthRefusal("FATAL_AUTH"); got != errFatalAuth {
		t.Fatalf("FATAL_AUTH → %v, ждали errFatalAuth", got)
	}
}

// Отказ по тарифу — отказ ступени, а не ключа: лестница идёт дальше.
func TestClassifyTierRelay(t *testing.T) {
	if got := classifyAuthRefusal("FATAL_TIER_RELAY"); got != errTierRelay {
		t.Fatalf("FATAL_TIER_RELAY → %v, ждали errTierRelay", got)
	}
	if fatalForLadder(errTierRelay) {
		t.Fatal("errTierRelay не должен обрывать лестницу")
	}
	if !strings.Contains(errTierRelay.Error(), "FATAL_TIER_RELAY") {
		t.Fatal("в тексте нет маркера FATAL_TIER_RELAY")
	}
	// Остальные отказы по-прежнему обрывают.
	if !fatalForLadder(errBoundOther) || !fatalForLadder(errDeactivated) {
		t.Fatal("ключевые отказы перестали обрывать лестницу")
	}
}
