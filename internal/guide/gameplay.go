// Copyright (c) 2026 Aristarh Ucolov.
//
// Plain-language descriptions for the fields of cfggameplay.json. The Gameplay
// editor is schema-free (it renders whatever keys the file holds, so it works
// on any DayZ version), which is robust but opaque: a raw key like
// "sprintStaminaModifierErc" tells an admin nothing. These strings give each
// well-known key a one-line explanation shown on hover.
//
// They live here, next to the guide, rather than in the i18n bundle: they are
// documentation, keyed by DayZ's own field names, and — like the guide — only
// English and Russian are written by hand, with English filling in for any
// other language. A key with no entry simply shows no tooltip.
//
// Keyed by the JSON leaf key exactly as DayZ writes it. Help() merges these in
// under a "gp." prefix so the panel reaches them with help("gp.<leafKey>").
package guide

// gameplayHelpEN is the English description for each known cfggameplay leaf key.
var gameplayHelpEN = map[string]string{
	"version": "Format version of cfggameplay.json. DayZ sets this; leave it as-is.",

	// GeneralData
	"disableBaseDamage":        "Base structures (walls, gates, fences, watchtowers) take no damage from anything.",
	"disableContainerDamage":   "Deployed storage (tents, barrels, crates, stashes) takes no damage.",
	"disableRespawnInSafeZone": "Players may not pick a spawn point inside a designated safe zone.",

	// BaseBuildingData / HologramData — the placement-preview checks. Disabling
	// one lets players place base parts the engine would normally block.
	"disableIsCollidingBBoxCheck":     "Skip the bounding-box overlap check when placing a base part — allows tighter, overlapping builds.",
	"disableIsCollidingPlayerCheck":   "Allow placing a base part even when a player stands where it would go.",
	"disableIsClippingRoofCheck":      "Allow base parts to clip through roofs.",
	"disableIsBaseViewCheck":          "Skip the line-of-sight check to the base area when placing.",
	"disableIsCollidingGeometryCheck": "Allow base parts to overlap terrain and world objects.",
	"disablePlaceOnSlopeCheck":        "Allow placing base parts on steep slopes.",
	"disableIsCollidingCheck":         "Skip the general collision check during construction.",

	// PlayerData
	"disableRespawnDialog": "Remove the death/respawn dialog — the player respawns immediately.",
	"disablePersonalLight": "Turn off the faint personal glow around each player in the dark (makes nights harder).",
	"DisablePersonalLight": "Turn off the faint personal glow around each player in the dark (makes nights harder).",
	"LightingConfig":       "Night brightness: 0 = bright (default), 1 = dark, 2 = very dark nights.",

	// StaminaData
	"sprintStaminaModifierErc":              "Stamina drain multiplier while sprinting with the weapon raised. Higher = tires faster.",
	"sprintStaminaModifierCrc":              "Stamina drain multiplier while sprinting crouched. Higher = tires faster.",
	"staminaWeightLimitThreshold":           "Carried weight (grams) above which the load starts cutting maximum stamina.",
	"staminaKgToStaminaPercentPenalty":      "How much each kilogram over the threshold reduces maximum stamina.",
	"staminaMax":                            "Size of the stamina pool. Higher = sprint longer.",
	"staminaMinCap":                         "Lowest the stamina cap can fall to under a heavy load.",
	"disableStaminaModifier":                "Ignore stamina entirely — infinite sprinting.",
	"disableStamina":                        "Ignore stamina entirely — infinite sprinting.",
	"disableStaminaDehydrationModifier":     "Dehydration no longer lowers stamina.",
	"disableStaminaHungerModifier":          "Hunger no longer lowers stamina.",

	// DrowningData
	"minStaminaToStartSwimming": "Minimum stamina needed before a player can begin swimming.",

	// UIData
	"use3DMap":            "Enable the in-world 3D map/marker system.",
	"disableWatch":        "Hide the wristwatch time readout.",
	"disableCompass":      "Disable the handheld compass.",
	"disableCompassGPS3D": "Disable the 3D GPS position marker shown with a compass + GPS.",

	// HitIndicationData
	"hitDirectionOverrideEnabled":         "Use these hit-indicator settings instead of the engine defaults.",
	"hitDirectionBehaviour":               "How the on-screen hit indicator behaves (static vs. moving).",
	"hitDirectionStyle":                   "Visual style of the hit indicator.",
	"hitDirectionMaxDuration":             "How long (ms) the hit indicator stays on screen.",
	"hitDirectionScatter":                 "Random angle spread added to the hit indicator direction.",
	"hitAnimationEnabled":                 "Play the on-hit character reaction animation.",

	// MapData
	"ignoreMapOwnership": "Players can open the map without carrying a physical map item.",
	"displayMapPosition": "Show the player's own position on the map.",
	"displayMapContour":  "Draw terrain contour lines on the map.",

	// WorldsData
	"lightingConfig":       "World night-darkness preset (same scale as LightingConfig).",
	"objectSpawnersArr":    "Extra static objects spawned into the world from JSON spawner files.",
	"environmentMinTemps":  "Twelve monthly minimum ambient temperatures (Jan–Dec).",
	"environmentMaxTemps":  "Twelve monthly maximum ambient temperatures (Jan–Dec).",

	// PlayerData root loadout registration
	"spawnGearPresetFiles": "Fresh-spawn loadout preset files. Edit these under the Player loadout page, not by hand.",
}

// gameplayHelpRU overrides the English text where a Russian translation exists.
var gameplayHelpRU = map[string]string{
	"version": "Версия формата cfggameplay.json. Задаётся DayZ — не трогайте.",

	"disableBaseDamage":        "Постройки базы (стены, ворота, заборы, вышки) не получают урона ни от чего.",
	"disableContainerDamage":   "Хранилища (палатки, бочки, ящики, схроны) не получают урона.",
	"disableRespawnInSafeZone": "Игроки не могут выбрать точку спавна внутри безопасной зоны.",

	"disableIsCollidingBBoxCheck":     "Пропустить проверку пересечения габаритов при установке детали базы — можно ставить впритык и внахлёст.",
	"disableIsCollidingPlayerCheck":   "Разрешить ставить деталь базы, даже если на её месте стоит игрок.",
	"disableIsClippingRoofCheck":      "Разрешить деталям базы проходить сквозь крыши.",
	"disableIsBaseViewCheck":          "Пропустить проверку прямой видимости зоны базы при установке.",
	"disableIsCollidingGeometryCheck": "Разрешить деталям базы пересекаться с рельефом и объектами мира.",
	"disablePlaceOnSlopeCheck":        "Разрешить ставить детали базы на крутых склонах.",
	"disableIsCollidingCheck":         "Пропустить общую проверку столкновений при строительстве.",

	"disableRespawnDialog": "Убрать окно смерти/возрождения — игрок возрождается сразу.",
	"disablePersonalLight": "Выключить слабое личное свечение вокруг игрока в темноте (ночи станут темнее).",
	"DisablePersonalLight": "Выключить слабое личное свечение вокруг игрока в темноте (ночи станут темнее).",
	"LightingConfig":       "Яркость ночи: 0 = светлая (по умолчанию), 1 = тёмная, 2 = очень тёмная.",

	"sprintStaminaModifierErc":          "Множитель траты выносливости на спринте с поднятым оружием. Больше = устаёт быстрее.",
	"sprintStaminaModifierCrc":          "Множитель траты выносливости на спринте в приседе. Больше = устаёт быстрее.",
	"staminaWeightLimitThreshold":       "Вес (в граммах), выше которого груз начинает урезать максимум выносливости.",
	"staminaKgToStaminaPercentPenalty":  "Насколько каждый килограмм сверх порога снижает максимум выносливости.",
	"staminaMax":                        "Размер запаса выносливости. Больше = дольше спринт.",
	"staminaMinCap":                     "Нижний предел, до которого может упасть лимит выносливости под тяжёлым грузом.",
	"disableStaminaModifier":            "Полностью отключить выносливость — бесконечный спринт.",
	"disableStamina":                    "Полностью отключить выносливость — бесконечный спринт.",
	"disableStaminaDehydrationModifier": "Обезвоживание больше не снижает выносливость.",
	"disableStaminaHungerModifier":      "Голод больше не снижает выносливость.",

	"minStaminaToStartSwimming": "Минимум выносливости, чтобы начать плыть.",

	"use3DMap":            "Включить систему 3D-карты/меток в мире.",
	"disableWatch":        "Скрыть показ времени по наручным часам.",
	"disableCompass":      "Отключить ручной компас.",
	"disableCompassGPS3D": "Отключить 3D-метку позиции (компас + GPS).",

	"hitDirectionOverrideEnabled": "Использовать эти настройки индикатора попаданий вместо стандартных.",
	"hitDirectionBehaviour":       "Поведение экранного индикатора попаданий (статичный / подвижный).",
	"hitDirectionStyle":           "Визуальный стиль индикатора попаданий.",
	"hitDirectionMaxDuration":     "Сколько (мс) индикатор попадания держится на экране.",
	"hitDirectionScatter":         "Случайный разброс угла у индикатора попадания.",
	"hitAnimationEnabled":         "Проигрывать анимацию реакции персонажа на попадание.",

	"ignoreMapOwnership": "Игроки могут открыть карту без физической карты в инвентаре.",
	"displayMapPosition": "Показывать собственную позицию игрока на карте.",
	"displayMapContour":  "Рисовать горизонтали рельефа на карте.",

	"lightingConfig":      "Пресет тёмности ночи мира (та же шкала, что и LightingConfig).",
	"objectSpawnersArr":   "Доп. статические объекты, добавляемые в мир из JSON-спавнеров.",
	"environmentMinTemps": "Двенадцать месячных минимальных температур (янв–дек).",
	"environmentMaxTemps": "Двенадцать месячных максимальных температур (янв–дек).",

	"spawnGearPresetFiles": "Файлы пресетов стартового набора. Правьте их на странице «Набор игрока», а не вручную.",
}

// GameplayFieldHelp returns cfggameplay field descriptions for a locale,
// Russian where available, English otherwise.
func GameplayFieldHelp(lang string) map[string]string {
	out := make(map[string]string, len(gameplayHelpEN))
	for k, v := range gameplayHelpEN {
		out[k] = v
	}
	if lang == "ru" {
		for k, v := range gameplayHelpRU {
			out[k] = v
		}
	}
	return out
}
