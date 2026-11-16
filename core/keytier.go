package core

import "meridian/engine/params"

// KeyTier — тариф ключа из /v1/key (params.KeyAnswer.Tier, engine-v1.10.10,
// кот 3): "basic" — «Базовый», пусто — премиум. Разбор поля — в движке
// (нет поля или незнакомое значение → премиум), здесь своего нет.
type KeyTier string

const (
	TierPremium KeyTier = ""
	TierBasic   KeyTier = params.TierBasic
)

// Basic — тариф «Базовый»: белого режима нет, DION не передаём (шлюз
// отказал бы FATAL_TIER_RELAY), остаётся чёрный режим с запасным VK.
func (t KeyTier) Basic() bool { return t == TierBasic }

// KeyTierOf — тариф из ответа CheckKey. ok=false — ответ без вердикта ok
// (служба молчала, ключ не годен): вызывающий держит последнее известное.
func KeyTierOf(ans *params.KeyAnswer) (tier KeyTier, ok bool) {
	if ans == nil || ans.NoVerdict || ans.State != params.KeyActive {
		return TierPremium, false
	}
	return KeyTier(ans.Tier), true
}
