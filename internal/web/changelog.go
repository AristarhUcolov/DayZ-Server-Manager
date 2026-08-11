// Copyright (c) 2026 Aristarh Ucolov.
//
// In-panel changelog. Shown once automatically after the exe is updated (the
// UI remembers the last version it displayed), and reachable any time by
// clicking the version in the sidebar. Notes are kept in English and Russian —
// the app's two primary languages, matching the guide — and other locales fall
// back to English.
//
// To cut a release: prepend an entry. Keep the notes short and user-facing.
package web

import "net/http"

type changelogEntry struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Notes   []string `json:"notes"`
}

type changelogRelease struct {
	Version string
	Date    string
	EN      []string
	RU      []string
}

// changelog is the release history, newest first.
var changelog = []changelogRelease{
	{
		Version: "0.26.0", Date: "2026-08-11",
		EN: []string{
			"Player profile — click any name to open an admin panel: identity and stats, recent combat, note & watch, and ban / unban / kick / message.",
			"Smart restarts — restart only when the server is empty (defer while players are online), and restart when the DayZ process memory passes a threshold.",
			"Announcement presets — save quick broadcast messages and send them to everyone in one click from the RCon page.",
			"Attachments now support random-preset references (the gear ⚙): add a preset from cfgrandompresets.xml, with name autocomplete, alongside explicit items.",
			"Maps got a background: a built-in schematic Chernarus is drawn by default, and you can upload your own top-down map picture (official or modded) from any map page — it then shows automatically for that world.",
		},
		RU: []string{
			"Профиль игрока — клик по нику открывает админ-панель: идентификация и статистика, недавние бои, заметка и watch, бан / разбан / кик / сообщение.",
			"Умные рестарты — рестарт только когда сервер пуст (отложить, пока есть игроки) и рестарт при превышении памяти процессом DayZ.",
			"Пресеты анонсов — сохраняйте быстрые сообщения и отправляйте их всем в один клик со страницы RCon.",
			"В Обвесах теперь есть ссылки на рандом-пресеты (шестерёнка ⚙): добавляйте пресет из cfgrandompresets.xml с автоподсказкой имени, рядом с явными предметами.",
			"У карты появился фон: по умолчанию рисуется схематичная Chernarus, а своё топ-даун изображение карты (официальное или модовское) можно загрузить с любой страницы карты — дальше оно показывается автоматически для этого мира.",
		},
	},
	{
		Version: "0.25.0", Date: "2026-08-11",
		EN: []string{
			"New map pages — a schematic map (Chernarus, Livonia, Sakhal) with a 1 km grid and landmarks.",
			"Heatmap — where players die and fights happen, from the admin log, split into PvP, environment and suicides.",
			"Event spawn editor — place and move cfgeventspawns.xml points on the map, with a check for events that have no spawn points (they never spawn) or points with no matching event.",
			"Live map — the last-known position of online players (positions come from connect/combat/chat, so a dot can lag; older than 5 min is dimmed).",
			"globals.xml form — a friendly editor with a plain-language tooltip per known variable.",
			"Where to find — type an item's class name to see its category, locations, tier and nominal.",
			"Random presets library — cfgrandompresets.xml with each item's real spawn chance.",
			"Mod loot check — which installed mods ship loot that isn't in your economy (so it never spawns).",
		},
		RU: []string{
			"Новые страницы с картой — схематичная карта (Chernarus, Livonia, Sakhal) с сеткой 1 км и ландмарками.",
			"Тепловая карта — где гибнут игроки и идут бои, из админ-лога, с разбивкой на PvP, окружение и суициды.",
			"Редактор точек событий — расставляйте и двигайте точки cfgeventspawns.xml на карте, с проверкой событий без точек (не заспавнятся) и точек без события.",
			"Живая карта — последняя известная позиция онлайн-игроков (позиции берутся из входа/боя/чата, поэтому точка может отставать; старше 5 мин приглушены).",
			"Форма globals.xml — удобный редактор с понятной подсказкой у каждой известной переменной.",
			"«Где найти» — введите класс предмета и увидите его категорию, локации, тир и nominal.",
			"Библиотека рандом-пресетов — cfgrandompresets.xml с реальным шансом каждого предмета.",
			"Проверка лута модов — какие установленные моды везут лут, которого нет в вашей экономике (и он не спавнится).",
		},
	},
	{
		Version: "0.24.0", Date: "2026-08-10",
		EN: []string{
			"New Leaderboard page — rank players by playtime, kills, K/D or sessions, with medals for the top three.",
			"Players page gained a watchlist and per-player notes — flag someone with a star and jot down why; both survive name changes.",
			"New Config profiles page — save the whole configuration as a named profile and switch between them in one click.",
			"New global config Search — find any class name, setting or value across every config file, with a jump straight to the line.",
			"New Loot economy page — see nominal by category, items by usage and tier, and the top items at a glance.",
			"CE tuning presets — scale spawn amounts across a types file (×2 / ×1.5 / ×0.5 / ×0.25) in one click, keeping the min ≤ nominal rule.",
		},
		RU: []string{
			"Новая страница «Лидерборд» — рейтинг игроков по времени в игре, убийствам, K/D или сессиям, с медалями для первой тройки.",
			"На странице игроков появились список наблюдения и заметки — отметьте игрока звездой и запишите причину; и то, и другое переживает смену ника.",
			"Новая страница «Профили конфигов» — сохраните всю конфигурацию как именованный профиль и переключайтесь между ними в один клик.",
			"Новый глобальный поиск по конфигам — найдите любой класс, параметр или значение во всех файлах, с переходом сразу к строке.",
			"Новая страница «Экономика лута» — nominal по категориям, предметы по usage и уровню, топ предметов с одного взгляда.",
			"Пресеты тюнинга CE — масштабируйте количество спавна во всём файле types (×2 / ×1.5 / ×0.5 / ×0.25) в один клик, сохраняя правило min ≤ nominal.",
		},
	},
	{
		Version: "0.23.1", Date: "2026-08-10",
		EN: []string{
			"Two new guide chapters — Player loadout and Server health — with screenshots, in all 11 languages.",
			"Linux internals hardened: the /proc parsers behind the process metrics are now unit-tested.",
		},
		RU: []string{
			"Две новые главы инструкции — «Стартовый набор» и «Здоровье сервера» — со скриншотами, на всех 11 языках.",
			"Укреплены Linux-внутренности: парсеры /proc за метриками процесса теперь покрыты юнит-тестами.",
		},
	},
	{
		Version: "0.23.0", Date: "2026-08-10",
		EN: []string{
			"Linux support — the manager now runs natively on Linux, with a dayz-manager-linux binary in releases.",
			"New Sponsors page — a thank-you to the people funding development, with a way for anonymous sponsors to claim their name.",
			"This “What's new” window, shown once after each update.",
			"Player loadout presets can be exported to a file and imported back — share loadouts between servers.",
			"The Players table now shows First seen alongside playtime and last seen.",
		},
		RU: []string{
			"Поддержка Linux — менеджер работает на Linux нативно, в релизах есть бинарник dayz-manager-linux.",
			"Новый раздел «Спонсоры» — благодарность тем, кто поддерживает развитие, и способ для анонимов указать своё имя.",
			"Это окно «Что нового», показывается один раз после каждого обновления.",
			"Пресеты набора игрока можно выгрузить в файл и загрузить обратно — делитесь лоадаутами между серверами.",
			"В таблице игроков теперь есть «Первый вход» рядом с наигранным и последним входом.",
		},
	},
	{
		Version: "0.22.1", Date: "2026-08-10",
		EN: []string{
			"Fixed the uneven row-hover on the Attachments contents column, and the cramped auto-fix note in the Validator.",
			"Validator auto-fix now also registers unregistered moded_types files in cfgeconomycore.xml — the top cause of custom loot not spawning.",
			"Validator results are sorted most-severe first with a summary.",
		},
		RU: []string{
			"Исправлена неровная подсветка строки в колонке «Содержимое» и поджатая подсказка авто-исправления в Валидаторе.",
			"Авто-исправление валидатора теперь ещё и регистрирует незарегистрированные файлы moded_types в cfgeconomycore.xml — главная причина, почему кастомный лут не спавнится.",
			"Результаты валидатора отсортированы по важности, добавлена сводка.",
		},
	},
	{
		Version: "0.22.0", Date: "2026-07-28",
		EN: []string{
			"Loadout test roll — sample one spawn and see exactly what a player gets.",
			"Config history page — every backup the manager took, grouped by file, with diff and one-click restore.",
			"Server health page + one-click cleanup of logs and old backups.",
			"Private message to a single player from the RCon page.",
		},
		RU: []string{
			"Пробный спавн набора — прокрутить один спавн и увидеть, что именно получит игрок.",
			"Страница «История» — все бэкапы менеджера, сгруппированы по файлу, со сравнением и откатом в один клик.",
			"Страница «Здоровье сервера» + очистка логов и старых бэкапов в один клик.",
			"Личное сообщение конкретному игроку со страницы RCon.",
		},
	},
	{
		Version: "0.21.0", Date: "2026-07-27",
		EN: []string{
			"Fresh spawns no longer arrive in worn gear — the loadout now writes an explicit pristine condition.",
			"Every Gameplay field gained a plain-language tooltip, plus a Reset to defaults button.",
			"The mod-config editor no longer lists server logs and crash dumps.",
		},
		RU: []string{
			"Новые игроки больше не появляются в изношенном луте — набор теперь пишет явное состояние «новое».",
			"У каждого поля Геймплея появилась понятная подсказка и кнопка «Сбросить к умолчаниям».",
			"Редактор конфигов модов больше не показывает логи сервера и краш-дампы.",
		},
	},
}

func (h *handlers) changelogList(w http.ResponseWriter, r *http.Request) {
	ru := r.URL.Query().Get("lang") == "ru"
	out := make([]changelogEntry, 0, len(changelog))
	for _, c := range changelog {
		notes := c.EN
		if ru && len(c.RU) > 0 {
			notes = c.RU
		}
		out = append(out, changelogEntry{Version: c.Version, Date: c.Date, Notes: notes})
	}
	writeJSON(w, map[string]interface{}{"current": h.app.Version, "releases": out})
}
