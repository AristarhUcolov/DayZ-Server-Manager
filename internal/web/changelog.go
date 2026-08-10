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
