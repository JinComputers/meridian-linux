package core

import "meridian/engine/params"

// Реэкспорт того, что реально определено в engine-params-v1.0.0 (кот 3,
// входит в текущий тег engine-v1.1.0). Получение параметров — не наш
// код (решение владельца 24.09): core только использует готовый клиент,
// своей реализации похода за /v1/params здесь нет и не будет.
type (
	ParamsClient = params.Client
	ParamsConfig = params.Config
	ParamsAnswer = params.Answer
	ParamsError  = params.Error
	ParamsNode   = params.Node
)

// NewParamsClient — тонкая обёртка над params.New, чтобы вызывающему
// коду (cmd/) не нужно было импортировать meridian/engine/params
// напрямую, только core.
func NewParamsClient(cfg ParamsConfig) *ParamsClient {
	return params.New(cfg)
}

// Реэкспорт того, что реально определено в engine-params-v1.1.0 (кот 3,
// 24.09 вечер): сборка лестницы транспорта из ответа /v1/params —
// BuildLadder, тот же порядок, что у Android (MeridianVpnService.
// buildLadder). Решение владельца 24.09 больше не «жди тега» — тег
// пришёл, подключаем как есть, своей версии не пишем.
type (
	LadderOptions = params.LadderOptions
	LadderInfo    = params.LadderInfo
	LadderMode    = params.Mode
)

const (
	LadderModeAuto       = params.ModeAuto
	LadderModeDirectOnly = params.ModeDirectOnly
	LadderModeRelayOnly  = params.ModeRelayOnly
)

// DefaultTURNBackup — запасной адрес релея (тот же, что TURN_ADDR_BACKUP
// у Android) — пустой LadderOptions.TURNBackup берёт его сам, реэкспорт
// только для справки/лога.
const DefaultTURNBackup = params.DefaultTURNBackup

// BuildLadder — тонкая обёртка над params.BuildLadder.
func BuildLadder(answer *ParamsAnswer, opts LadderOptions) (*Ladder, *LadderInfo, error) {
	return params.BuildLadder(answer, opts)
}

// ParseHashes — несколько ссылок VK на звонки (или голых хешей) из
// одной строки → нормализованные хеши для LadderOptions.Hashes.
// Разделители: запятая, точка с запятой, перевод строки, таб, пробел.
var ParseHashes = params.ParseHashes

// NormalizeHash — одна ссылка/хеш → хеш ("" — не разобрано).
var NormalizeHash = params.NormalizeHash

// ParamsKindUnavailable — служба не ответила ни по одному адресу
// (RunControlConnect → FailureAPIUnavailable).
const ParamsKindUnavailable = params.KindUnavailable
