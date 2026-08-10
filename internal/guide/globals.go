// Copyright (c) 2026 Aristarh Ucolov.
//
// Plain-language descriptions for the variables in globals.xml (DayZ central
// economy). Like the cfggameplay help, these live next to the guide, keyed by
// DayZ's own variable names, English with Russian overrides. Help() merges them
// under a "gl." prefix, so the panel reaches them with help("gl.<VarName>").
// A variable with no entry simply shows no tooltip.
package guide

var globalsHelpEN = map[string]string{
	"AnimalMaxCount":              "Maximum number of animals alive on the map at once.",
	"ZombieMaxCount":              "Maximum number of infected alive on the map at once.",
	"ZoneSpawnDist":               "Distance (m) from a player at which dynamic infected/animal zones spawn.",
	"IdleModeCountdown":           "Seconds with no players connected before the economy goes idle and stops respawning loot.",
	"IdleModeStartup":             "Start the economy in idle mode until the first player joins (1 = yes).",
	"RespawnAttempt":              "How many times the engine retries a failed loot respawn per cycle.",
	"RespawnTypes":                "How many loot types are processed for respawn each cycle.",
	"TimeLogin":                   "Seconds a player waits on the loading screen when logging in.",
	"TimeLogout":                  "Seconds a player must stay in-world after choosing to log out (anti-combat-log).",
	"TimeHopping":                 "Extra login delay (s) applied to players who server-hop, to discourage it.",
	"TimePenalty":                 "Additional wait (s) for logging out while recently in combat.",
	"LoginTimeMax":               "Maximum time (s) the login can take before the attempt is dropped.",
	"LogoutTimeMax":              "Maximum logout wait (s).",
	"WorldWetTempUpdate":          "How often (s) the wetness and temperature of world items is updated.",
	"CleanupAvoidance":            "Factor that keeps freshly spawned loot from being cleaned up too soon.",
	"CleanupLifetimeDefault":      "Default seconds a dropped item stays before cleanup when its type sets no lifetime.",
	"CleanupLifetimeDeadPlayer":   "Seconds a dead player's body and its gear stay before removal.",
	"CleanupLifetimeDeadInfected": "Seconds a dead infected body stays before removal.",
	"CleanupLifetimeDeadAnimal":   "Seconds a dead animal carcass stays before removal.",
	"CleanupLifetimeRuined":       "Seconds a ruined item stays on the ground before cleanup.",
	"CleanupLifetimeLimit":        "Lower bound (s) the cleanup uses for very short item lifetimes.",
	"FlagRefreshFrequency":        "How often (s) a raised territory flag refreshes the lifetime of nearby deployed items.",
	"FlagRefreshMaxDuration":      "Maximum lifetime (s) a flag can refresh items to — the base-decay cap (vanilla ≈ 45 days).",
	"InitialSpawn":                "Amount of initial loot spawned when the economy first starts.",
	"SpawnInitial":                "Amount of initial loot spawned when the economy first starts.",
}

var globalsHelpRU = map[string]string{
	"AnimalMaxCount":              "Максимальное число живых животных на карте одновременно.",
	"ZombieMaxCount":              "Максимальное число живых заражённых на карте одновременно.",
	"ZoneSpawnDist":               "Дистанция (м) от игрока, на которой спавнятся динамические зоны заражённых/животных.",
	"IdleModeCountdown":           "Секунд без подключённых игроков, после чего экономика уходит в простой и перестаёт респавнить лут.",
	"IdleModeStartup":             "Запускать экономику в режиме простоя до первого игрока (1 = да).",
	"RespawnAttempt":              "Сколько раз движок повторяет неудавшийся респавн лута за цикл.",
	"RespawnTypes":                "Сколько типов лута обрабатывается на респавн за цикл.",
	"TimeLogin":                   "Секунд ожидания на экране загрузки при входе.",
	"TimeLogout":                  "Секунд, которые игрок остаётся в мире после выбора выхода (защита от combat-log).",
	"TimeHopping":                 "Доп. задержка входа (с) для игроков, прыгающих по серверам, чтобы отбить желание.",
	"TimePenalty":                 "Доп. ожидание (с) за выход из игры сразу после боя.",
	"LoginTimeMax":               "Максимальное время входа (с), после которого попытка сбрасывается.",
	"LogoutTimeMax":              "Максимальное ожидание выхода (с).",
	"WorldWetTempUpdate":          "Как часто (с) обновляется влажность и температура предметов в мире.",
	"CleanupAvoidance":            "Коэффициент, не дающий только что заспавненному луту быть убранным слишком рано.",
	"CleanupLifetimeDefault":      "Секунд, сколько лежит выброшенный предмет до уборки, если у типа не задан lifetime.",
	"CleanupLifetimeDeadPlayer":   "Секунд, сколько тело мёртвого игрока с вещами лежит до удаления.",
	"CleanupLifetimeDeadInfected": "Секунд, сколько лежит труп заражённого до удаления.",
	"CleanupLifetimeDeadAnimal":   "Секунд, сколько лежит туша животного до удаления.",
	"CleanupLifetimeRuined":       "Секунд, сколько испорченный предмет лежит на земле до уборки.",
	"CleanupLifetimeLimit":        "Нижняя граница (с) для очень коротких lifetime при уборке.",
	"FlagRefreshFrequency":        "Как часто (с) поднятый флаг территории обновляет lifetime установленных рядом предметов.",
	"FlagRefreshMaxDuration":      "Максимальный lifetime (с), до которого флаг обновляет предметы — предел распада базы (ваниль ≈ 45 дней).",
	"InitialSpawn":                "Количество стартового лута при первом запуске экономики.",
	"SpawnInitial":                "Количество стартового лута при первом запуске экономики.",
}

// GlobalsFieldHelp returns globals.xml variable descriptions for a locale.
func GlobalsFieldHelp(lang string) map[string]string {
	out := make(map[string]string, len(globalsHelpEN))
	for k, v := range globalsHelpEN {
		out[k] = v
	}
	if lang == "ru" {
		for k, v := range globalsHelpRU {
			out[k] = v
		}
	}
	return out
}
