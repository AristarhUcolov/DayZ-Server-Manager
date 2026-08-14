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
		Version: "0.30.0", Date: "2026-08-14",
		EN: []string{
			"Real-time chat — the Chat page now shows in-game chat live, straight from the server's BattlEye RCon stream (before, only your outgoing broadcasts worked). A status badge shows whether the live feed is connected, and a clear message explains when RCon isn't set up or the server is offline.",
			"Live map — player dots now appear even when RCon can't confirm who's online, drawn from the last-known positions (marked as such) so the map is no longer blank. An honest note reminds you that vanilla DayZ only logs positions on connect, combat and chat.",
			"Maps that never go blank — Livonia and Sakhal now have their own built-in schematics (coastline, water); any other or modded world shows a clean placeholder inviting you to upload a top-down map image.",
			"Zoom & pan on every map — use the mouse wheel or the on-screen +/− buttons to zoom, and drag to pan, for precise point placement; world coordinates stay correct at any zoom.",
			"Every world supported — the manager recognises the map from the mission name (3 official plus ~50 popular modded maps: Namalsk, Deer Isle, Banov, Chiemsee, Rostow, Takistan, Esseker, Sarov, Nyheim…) and auto-registers any other. Set the real map yourself: upload an image or paste an image URL the manager downloads. No third-party map art is bundled — you point it at a source you have the right to use.",
			"Stability — the dashboard now says \"RCon not connected\" instead of a misleading \"0 players\", the performance graph no longer records a false 0 when RCon is unreachable, dead RCon connections are dropped promptly instead of stalling a command, and pages that read the admin log warn you when -adminlog is off.",
		},
		RU: []string{
			"Чат в реальном времени — страница «Чат» теперь показывает игровой чат вживую, прямо из потока BattlEye RCon сервера (раньше работала только отправка ваших сообщений). Бейдж показывает, подключён ли живой поток, а понятное сообщение объясняет, если RCon не настроен или сервер офлайн.",
			"Живая карта — точки игроков теперь появляются даже когда RCon не может подтвердить, кто онлайн: они берутся из последних известных позиций (с пометкой), и карта больше не пустая. Честная подсказка напоминает, что ванильный DayZ пишет позиции только при входе, бое и чате.",
			"Карты, которые больше не пустые — у Ливонии и Сахалина появились собственные встроенные схемы (береговая линия, вода); любой другой или модовый мир показывает аккуратный плейсхолдер с призывом загрузить топ-даун карту.",
			"Зум и панорама на каждой карте — колесом мыши или экранными кнопками +/− можно приближать, а перетаскиванием — двигать карту для точного размещения точек; мировые координаты остаются верными на любом масштабе.",
			"Поддержка всех карт — менеджер распознаёт карту по названию миссии (3 официальных плюс ~50 популярных модовых: Namalsk, Deer Isle, Banov, Chiemsee, Rostow, Takistan, Esseker, Sarov, Nyheim…), а любую другую заводит автоматически. Настоящую карту задаёте сами: загрузите картинку или вставьте URL (менеджер скачает). Чужие изображения карт в сборку не включаются — вы указываете источник, которым вправе пользоваться.",
			"Стабильность — на панели теперь пишется «RCon не подключён» вместо обманчивых «0 игроков», график производительности не записывает ложный 0 при недоступном RCon, мёртвые RCon-соединения закрываются сразу, а не подвешивают команду, и страницы, читающие админ-лог, предупреждают, если -adminlog выключен.",
		},
	},
	{
		Version: "0.29.0", Date: "2026-08-12",
		EN: []string{
			"Player spawns editor — a new page under Server that edits cfgplayerspawnpoints.xml on the map: switch between Fresh / Hop / Travel, see each named group as a coloured cluster, and click to add a point, drag to move, right-click to delete. Add/rename/delete groups and tune the spawn / generator / group parameters; the distance and grid settings round-trip untouched.",
			"Accessibility pass — every form field across the panel now has a programmatically-associated label (via for/id or aria-label, in all 11 languages), so screen readers announce what each input is. Icon buttons and images were already labelled.",
		},
		RU: []string{
			"Редактор точек спавна — новая страница в разделе «Сервер», правит cfgplayerspawnpoints.xml на карте: переключение Fresh / Hop / Travel, каждая именованная группа — цветной кластер, клик — добавить точку, перетащить — двигать, правый клик — удалить. Добавляйте/переименовывайте/удаляйте группы и настраивайте параметры спавна / генератора / групп; настройки дистанций и сетки сохраняются нетронутыми.",
			"Проход по доступности — у каждого поля формы в панели теперь есть программно связанная подпись (через for/id или aria-label, на всех 11 языках), чтобы скринридеры озвучивали назначение каждого поля. Иконки-кнопки и картинки уже были подписаны.",
		},
	},
	{
		Version: "0.28.0", Date: "2026-08-12",
		EN: []string{
			"Presets editor now works like Attachments — item class names autocomplete from your types.xml (mods included), unknown classes are flagged as you type, and the row layout matches (name / weight / real chance).",
			"Validator auto-fix handles more: it removes cross-file duplicate types (a moded_types entry redefining a base types.xml name — the base is kept) and deletes orphan spawn positions for events no longer in events.xml. Every fixed file keeps a .bak.",
			"Loot economy: the ×1 (restore) button now sits between ×1.5 and ×0.5 so the tuning row reads high to low.",
			"Sponsors moved to the second row of the sidebar, under Dashboard, with the section's normal icon colour — easy to find without scrolling.",
			"More hover help (Weather channels, Events fields, mod-loot columns, Leaderboard) and refreshed screenshots throughout.",
		},
		RU: []string{
			"Редактор пресетов теперь работает как Обвесы — имена классов автодополняются из вашего types.xml (включая моды), неизвестные классы подсвечиваются при вводе, а раскладка строки совпадает (имя / вес / реальный шанс).",
			"Авто-исправление валидатора умеет больше: убирает кросс-файловые дубликаты типов (запись moded_types, переопределяющая имя из базового types.xml — базовый остаётся) и удаляет осиротевшие спавн-позиции для событий, которых больше нет в events.xml. Для каждого исправленного файла сохраняется .bak.",
			"Экономика лута: кнопка ×1 (сброс) теперь между ×1.5 и ×0.5, чтобы ряд читался от большего к меньшему.",
			"«Спонсоры» переехали во второй ряд сайдбара, под «Панель», с обычным цветом иконки раздела — легко найти без прокрутки.",
			"Больше подсказок по наведению (каналы погоды, поля событий, колонки проверки лута модов, лидерборд) и обновлённые скриншоты.",
		},
	},
	{
		Version: "0.27.0", Date: "2026-08-11",
		EN: []string{
			"Presets editor — the old read-only \"Random presets\" page is now a full editor: create your own cargo/attachment presets or tweak the ready-made ones, then reference them by name in the Attachments editor.",
			"Loot economy ×1 — a new reset button restores a types file to the amounts it had before your first scaling, so ×2 / ×0.5 tuning is always reversible.",
			"Temporary bans — the player profile can now ban for a set number of days (1–30) as well as permanently, and shows the time left on the ban badge.",
			"Disk cleanup split — Server health now clears ADM logs and old wipe snapshots as their own buttons (keeping the newest of each), next to logs and backups.",
			"Validator does more — one-click auto-fix now removes duplicate <type> entries (keeping the first, comments preserved), and it flags event spawn positions that fall off the map.",
			"Live chat page + watchlist connect pings, scheduled config profiles (swap a saved profile at a daily time during a restart), and modded-map support (each world gets its own uploaded picture and an adjustable size).",
			"More hover help (ⓘ) on the Server and Loot economy pages, a new guide chapter on the loot economy & presets, refreshed screenshots throughout, and the Sponsors link moved up near the top so it is easy to find.",
		},
		RU: []string{
			"Редактор пресетов — бывшая страница «Рандом-пресеты» (только чтение) стала полноценным редактором: создавайте свои cargo/attachments-пресеты или правьте готовые, затем ссылайтесь на них по имени в Обвесах.",
			"×1 в экономике лута — новая кнопка сброса возвращает файл types к значениям до первого масштабирования, так что тюнинг ×2 / ×0.5 всегда обратим.",
			"Временные баны — в профиле игрока теперь можно забанить на срок в днях (1–30), а не только навсегда; остаток показывается на бейдже бана.",
			"Очистка диска подробнее — «Здоровье сервера» теперь чистит ADM-логи и старые снимки вайпов отдельными кнопками (оставляя новейший), рядом с логами и бэкапами.",
			"Валидатор умнее — авто-фикс в один клик убирает дубликаты <type> (оставляя первый, комментарии сохраняются) и отмечает спавн-позиции событий, вылетающие за карту.",
			"Страница живого чата + пинги подключения из watchlist, профили конфигов по расписанию (смена сохранённого профиля в заданное время во время рестарта) и поддержка модовских карт (у каждого мира своя загруженная картинка и настраиваемый размер).",
			"Больше подсказок по наведению (ⓘ) на страницах Сервер и Экономика лута, новая глава гайда про экономику лута и пресеты, обновлённые скриншоты и ссылка «Спонсоры», поднятая наверх, чтобы её было легко найти.",
		},
	},
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
