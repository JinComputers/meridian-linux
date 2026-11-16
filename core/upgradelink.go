package core

import (
	"crypto/sha256"
	"encoding/hex"
)

// upgradeBotURL — повышение с «Базового» до Премиума в боте (кот 2,
// решение владельца 16.10): оплата и расчёт там, приложению — только ссылка.
const upgradeBotURL = "https://t.me/jincomputers_bot?start=up_"

// UpgradeID — первые 16 hex от sha256("meridian-up-v1:"+ключ). Сам ключ
// в ссылку не попадает; бот находит ключ по этому id.
func UpgradeID(key string) string {
	sum := sha256.Sum256([]byte("meridian-up-v1:" + key))
	return hex.EncodeToString(sum[:])[:16]
}

// upgradeAppURL — то же напрямую в приложение Telegram (как на Android).
const upgradeAppURL = "tg://resolve?domain=jincomputers_bot&start=up_"

// UpgradeAppURL — ссылка кнопки для приложения Telegram; без него — UpgradeURL.
func UpgradeAppURL(key string) string { return upgradeAppURL + UpgradeID(key) }

// UpgradeURL — ссылка кнопки «Повысить до Премиума».
func UpgradeURL(key string) string { return upgradeBotURL + UpgradeID(key) }
