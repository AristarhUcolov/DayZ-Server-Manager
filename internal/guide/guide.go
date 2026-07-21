// Copyright (c) 2026 Aristarh Ucolov.
//
// The in-panel beginner's guide.
//
// Long-form documentation deliberately lives OUTSIDE the i18n bundle: that
// bundle is UI chrome and every locale is required to translate all of it
// (enforced by a parity test). A multi-page manual is a different beast — it
// ships fully in English and Russian and falls back to English elsewhere, so
// adding a language never means writing a manual first.
package guide

import "strings"

// Step is one numbered instruction inside a chapter. Route, when set, renders
// as a button that jumps straight to that section of the panel.
type Step struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Route string `json:"route,omitempty"`
}

// Chapter is one topic of the guide.
type Chapter struct {
	ID    string `json:"id"`
	Icon  string `json:"icon"` // id of an inline SVG sprite symbol
	Title string `json:"title"`
	Intro string `json:"intro"`
	// Image is a screenshot of the section, served from the embedded static
	// files. Regenerate with the capture script when the UI changes.
	Image string   `json:"image,omitempty"`
	Steps []Step   `json:"steps,omitempty"`
	Tips  []string `json:"tips,omitempty"` // gotchas worth calling out
}

// Get returns the guide for a locale, falling back to English.
func Get(lang string) []Chapter {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "ru":
		return ru()
	default:
		return en()
	}
}

// Help returns the hover-tooltip texts for a locale, falling back to English
// per key — a locale that translates half of them still shows the other half
// in English rather than a raw key.
func Help(lang string) map[string]string {
	base := helpEN()
	if strings.EqualFold(strings.TrimSpace(lang), "ru") {
		out := make(map[string]string, len(base))
		for k, v := range base {
			out[k] = v
		}
		for k, v := range helpRU() {
			out[k] = v
		}
		return out
	}
	return base
}

func helpEN() map[string]string {
	return map[string]string{
		"types.nominal":      "How many of this item the server tries to keep in the world at once. 0 means it never spawns.",
		"types.min":          "When the count drops to this, the server queues a respawn. Must not exceed nominal — DayZ clamps it and logs a warning.",
		"types.lifetime":     "Seconds an untouched item survives before it despawns. 3888000 is 45 days.",
		"types.restock":      "Seconds the server waits before topping this item back up. 0 means immediately.",
		"types.usage":        "Which building types the item may spawn in (Military, Police, Farm…). No usage means no place to spawn.",
		"types.value":        "Loot tier: Tier1 is the coast, Tier4 is deep inland and military. Controls how far from spawn the item appears.",
		"types.quant":        "Fill level as a percentage for items with a quantity (drinks, fuel). Use -1 to disable, otherwise 0–100.",
		"types.bulk":         "Applies these values to every selected row at once. Only the fields you fill in are changed.",
		"mods.serverSide":    "Loads the mod with -serverMod instead of -mod: it runs on the server and clients neither need nor get it. For admin tools only — a loot or content mod set server-side will not work.",
		"mods.order":         "DayZ loads mods left to right. Frameworks must come first: @CF before anything depending on it, Expansion Core before its addons. Wrong order is a common reason a server refuses to boot.",
		"mods.update":        "Copies newer files from the client's !Workshop into the server. The manager cannot download mods — subscribe in Steam first.",
		"mods.scan":          "Looks inside the mod for a types.xml and registers it in cfgeconomycore.xml, so the mod's loot actually spawns.",
		"weather.transition": "How gradually weather changes. Smooth ramps over about half an hour in small steps, like vanilla DayZ. Fast can swing the full range in two minutes, which is what makes weather feel like a light switch.",
		"weather.dynamic":    "Off pins the weather where you set it. On lets the server drift it on its own within the limits.",
		"weather.accel":      "Multiplies the passage of in-game time. 12 means a full day takes two real hours. Night is separate so you can keep nights short.",
		"weather.storm":      "Only has an effect when rain and overcast are high enough for a storm to trigger at all.",
		"rcon.password":      "Written into battleye/beserver_x64.cfg. BattlEye reads that file only at launch, so a new password does nothing until the next restart.",
		"attach.slotChance":  "Probability that this slot spawns anything at all. 1.0 = always, 0.35 = 35% of the time.",
		"attach.itemWeight":  "A relative weight, not a percentage — DayZ picks exactly ONE item from the slot. Weights 60 and 40 mean 60% and 40% of the slot's own chance.",
		"settings.restart":   "Restarts the server on a schedule. Players get RCon warnings beforehand, which needs an RCon password to be set.",
		"settings.watchdog":  "Restarts the server if it exits on its own. After three crashes in five minutes it stops and shows a banner — a crash loop needs a human, not another restart.",
		"settings.backup":    "Zips your configs and mission files on a schedule and keeps the newest N. Separately, every file the panel overwrites gets a timestamped .bak next to it.",
		"settings.exposure":  "Local means only this PC can open the panel. Local network makes it reachable from your phone. The panel has no login — never expose it straight to the internet.",
		"settings.adminlog":  "Enables -adminlog, which writes the .ADM file. The Players page and the killfeed are built from it and stay empty without it.",
		"validator.autofix":  "Adds unknown usage/value/tag/category names into cfglimitsdefinition.xml. This is usually what makes modded loot start spawning — the names must exist there or DayZ ignores the entry.",
		"wipe":               "Deletes saved world state: players, vehicles, bases. Loot tables and configs are untouched. Folders are moved aside rather than erased, so a mistake can be undone by hand.",
	}
}

func helpRU() map[string]string {
	return map[string]string{
		"types.nominal":      "Сколько таких предметов сервер старается держать в мире одновременно. 0 — предмет не спавнится вообще.",
		"types.min":          "Когда количество падает до этого значения, сервер ставит респавн в очередь. Не должно превышать nominal — DayZ обрежет и напишет предупреждение.",
		"types.lifetime":     "Сколько секунд нетронутый предмет живёт до исчезновения. 3888000 — это 45 дней.",
		"types.restock":      "Сколько секунд сервер ждёт перед пополнением этого предмета. 0 — сразу.",
		"types.usage":        "В каких типах зданий предмет может появиться (Military, Police, Farm…). Без usage предмету негде спавниться.",
		"types.value":        "Тир лута: Tier1 — побережье, Tier4 — вглубь карты и военка. Определяет, как далеко от спавна встречается предмет.",
		"types.quant":        "Заполненность в процентах для предметов с количеством (напитки, топливо). -1 — выключено, иначе 0–100.",
		"types.bulk":         "Применяет значения сразу ко всем выделенным строкам. Меняются только заполненные поля.",
		"mods.serverSide":    "Загружает мод через -serverMod вместо -mod: он работает на сервере, клиентам не нужен и не выдаётся. Только для админ-инструментов — мод на лут или контент так работать не будет.",
		"mods.order":         "DayZ грузит моды слева направо. Фреймворки должны идти первыми: @CF раньше всего, что от него зависит, Expansion Core раньше своих аддонов. Неверный порядок — частая причина, почему сервер не стартует.",
		"mods.update":        "Копирует свежие файлы из !Workshop клиента на сервер. Менеджер не качает моды — сначала подпишитесь в Steam.",
		"mods.scan":          "Ищет в моде types.xml и прописывает его в cfgeconomycore.xml, чтобы лут мода действительно спавнился.",
		"weather.transition": "Насколько постепенно меняется погода. «Плавно» — переход примерно за полчаса маленькими шагами, как в ванильном DayZ. «Быстро» способно пройти весь диапазон за две минуты — именно от этого погода ощущается как выключатель.",
		"weather.dynamic":    "Выключено — погода стоит там, где вы её задали. Включено — сервер сам меняет её в пределах лимитов.",
		"weather.accel":      "Умножает ход игрового времени. 12 означает, что полные сутки проходят за два реальных часа. Ночь настраивается отдельно, чтобы её можно было укоротить.",
		"weather.storm":      "Работает только тогда, когда дождя и облачности хватает, чтобы гроза вообще началась.",
		"rcon.password":      "Записывается в battleye/beserver_x64.cfg. BattlEye читает этот файл только при запуске, поэтому новый пароль ничего не даст до следующего рестарта.",
		"attach.slotChance":  "Вероятность, что в этом слоте вообще что-то появится. 1.0 — всегда, 0.35 — в 35% случаев.",
		"attach.itemWeight":  "Это относительный вес, а не проценты: DayZ выбирает РОВНО ОДИН предмет из слота. Веса 60 и 40 означают 60% и 40% от собственного шанса слота.",
		"settings.restart":   "Перезапускает сервер по расписанию. Игроки получают предупреждения через RCon — для этого нужен заданный пароль RCon.",
		"settings.watchdog":  "Поднимает сервер, если он выключился сам. После трёх падений за пять минут останавливается и показывает баннер: цикл падений требует человека, а не ещё одного рестарта.",
		"settings.backup":    "Складывает конфиги и файлы миссии в zip по расписанию и хранит N последних. Отдельно: каждый файл, который панель перезаписывает, получает рядом .bak с меткой времени.",
		"settings.exposure":  "«Только локально» — панель открывается лишь на этом ПК. «Локальная сеть» — доступна с телефона. Логина у панели нет — никогда не выставляйте её напрямую в интернет.",
		"settings.adminlog":  "Включает -adminlog, который пишет файл .ADM. Раздел «Игроки» и килфид строятся из него и без него остаются пустыми.",
		"validator.autofix":  "Вносит неизвестные usage/value/tag/category в cfglimitsdefinition.xml. Обычно именно после этого модовский лут начинает спавниться — эти имена обязаны там быть, иначе DayZ игнорирует запись.",
		"wipe":               "Удаляет сохранённое состояние мира: игроков, машины, базы. Таблицы лута и конфиги не трогаются. Папки переносятся в сторону, а не стираются, поэтому ошибку можно откатить руками.",
	}
}

func en() []Chapter {
	return []Chapter{
		{
			ID: "start", Icon: "i-dashboard", Title: "Getting started",
			Intro: "The manager is a single .exe that runs next to your DayZ server and gives you a web panel instead of Notepad and .bat files. It never installs anything system-wide — delete the .exe and everything is as it was.",
			Image: "/img/dashboard.webp",
			Steps: []Step{
				{Title: "Put the exe in the server folder", Body: "Copy dayz-manager.exe next to DayZServer_x64.exe and serverDZ.cfg, then run it. A console window opens and your browser lands on http://127.0.0.1:8787."},
				{Title: "Point it at your DayZ client", Body: "On first run you are asked for the folder of the DayZ *client* — the game install that contains the !Workshop folder. That is where Steam downloads mods, and it is the only way the manager can copy them into your server.", Route: "settings"},
				{Title: "Start the server", Body: "Press Start on the dashboard. If DayZServer_x64.exe is missing or a mod is broken, the server exits and you will see it in the Logs section.", Route: "dashboard"},
			},
			Tips: []string{
				"The panel has no login. Keep it on Local unless you trust your network; for remote access put a reverse proxy with a password in front of it.",
				"Almost every file editor refuses to save while the server is running — DayZ holds locks on its own files. Stop the server first; the panel tells you when this is the reason.",
			},
		},
		{
			ID: "mods", Icon: "i-mods", Title: "Mods",
			Intro: "DayZ mods live in your client's !Workshop folder after you subscribe on Steam. A server needs its own copy of each mod, plus the mod's .bikey in the server's keys folder, plus the mod listed in the -mod launch parameter. The manager does all three.",
			Image: "/img/mods.webp",
			Steps: []Step{
				{Title: "Subscribe in Steam first", Body: "The manager copies mods, it does not download them. Subscribe to the mod in the Steam Workshop and let the DayZ launcher pull it into !Workshop."},
				{Title: "Install into the server", Body: "Open Mods — everything in !Workshop is listed. Install copies the mod into the server folder and syncs its .bikey automatically. Sync all does the whole list in one click.", Route: "mods"},
				{Title: "Enable it for launch", Body: "Installing is not the same as loading. Flip the Mods switch on a row to add it to -mod. Use the Server-side switch only for mods that clients must NOT have (admin tools)."},
				{Title: "Mind the load order", Body: "Drag rows in Load order so frameworks come first — @CF before anything that depends on it, Expansion Core before its addons. Wrong order is a very common cause of a server that will not boot."},
			},
			Tips: []string{
				"Update copies the newer files from !Workshop into the server. Turn on Update mods on every restart in Settings to keep them in sync automatically.",
				"Uninstalling a mod only deletes its .bikey if no other installed mod uses the same key, so shared keys (Expansion) stay intact.",
				"If a mod ships loot, use Scan for loot types on its row — it finds the mod's types.xml and registers it for you.",
			},
		},
		{
			ID: "economy", Icon: "i-types", Title: "Loot and economy",
			Intro: "DayZ's central economy decides what exists in the world and how much of it. types.xml is the master list: one entry per item class, with how many should exist and where they may appear. Getting these numbers wrong is the usual reason a server feels empty or drowning in loot.",
			Image: "/img/types.webp",
			Steps: []Step{
				{Title: "Understand the four numbers", Body: "nominal is the target amount the server tries to keep in the world. min is the floor that triggers a respawn. lifetime is how long an untouched item survives, in seconds. restock is how long the server waits before topping the item back up. Set nominal to 0 and the item never spawns.", Route: "types"},
				{Title: "Edit in the table", Body: "nominal, min, lifetime and category are editable straight in the table — edited rows are highlighted until you press Save changes. Use Edit for the full item: usages, values, tags and flags."},
				{Title: "Usage and value = where it spawns", Body: "usage is the building type (Military, Police, Farm…), value is the loot tier (Tier1 is coastal, Tier4 is deep inland / military). An item with no usage has nowhere to spawn. Both names must exist in cfglimitsdefinition.xml."},
				{Title: "Use presets for bulk work", Body: "Select rows, then click a spawn preset to merge its usage/value/tags into all of them at once — far faster than editing hundreds of entries by hand."},
			},
			Tips: []string{
				"Keep your own items in a custom types file rather than editing the vanilla types.xml — the manager registers it in cfgeconomycore.xml for you, and a game update cannot overwrite it.",
				"Run the Validator after any bulk edit. Its Auto-fix adds unknown usage/value/tag names into cfglimitsdefinition.xml, which is exactly what makes modded loot start spawning.",
			},
		},
		{
			ID: "attachments", Icon: "i-attach", Title: "Weapon attachments",
			Intro: "cfgspawnabletypes.xml decides what a weapon is carrying when it spawns — a magazine, an optic, a buttstock. It is built from slots, and the numbers mean two different things, which is the part everybody gets wrong.",
			Image: "/img/attachments.webp",
			Steps: []Step{
				{Title: "A slot is one roll of the dice", Body: "Each attachments slot has a chance: 1.0 means it always spawns something, 0.35 means 35% of the time.", Route: "attachments"},
				{Title: "Items inside a slot compete", Body: "The number on each item is a relative weight, not a percentage — DayZ picks exactly ONE item from the slot. Weights of 60 and 40 inside a 0.35 slot really mean 21% and 14%. The panel shows you that real number so you never have to work it out."},
				{Title: "Start from a template", Body: "Pick a weapon template (AKM, M4-A1, Mosin…), then adjust. Class names autocomplete from your own types.xml, and anything not found there is flagged — a typo is the usual reason an attachment never appears in game."},
			},
			Tips: []string{
				"One slot per part: one for magazines, one for optics, one for the stock. Putting a magazine and an optic in the same slot means the weapon gets one or the other, never both.",
				"Only the entry you edit is rewritten — your comments and formatting in the rest of the file are preserved.",
			},
		},
		{
			ID: "rcon", Icon: "i-rcon", Title: "RCon, players and announcements",
			Intro: "RCon is BattlEye's remote console: the live player list, kicks, bans and chat broadcasts. It needs a password that BattlEye reads from beserver_x64.cfg when the server starts.",
			Image: "/img/players.webp",
			Steps: []Step{
				{Title: "Set the password", Body: "Type one in the RCon section. The manager writes it into battleye/beserver_x64.cfg for you, creating the file if needed.", Route: "rcon"},
				{Title: "Restart the server", Body: "BattlEye only reads that file at launch, so a brand-new password does nothing until the next restart. This is normal and not a bug."},
				{Title: "Announce and schedule", Body: "Scheduled announcements fire at a time of day; interval announcements repeat every N minutes. Both go out over RCon, so they need the password to be live.", Route: "settings"},
			},
			Tips: []string{
				"Restart warnings use RCon too. Without a password the restart still happens — players just get no countdown.",
				"The Players section builds a history from the admin log: names, GUIDs, playtime, kills. It needs the -adminlog launch flag enabled in Settings.",
			},
		},
		{
			ID: "weather", Icon: "i-weather", Title: "Weather and time",
			Intro: "Weather is defined in the mission's cfgweather.xml, which DayZ reads once when the mission loads — so weather changes always need a server restart, and there is no vanilla way to schedule 'rain at 20:00' without a mod.",
			Image: "/img/weather.webp",
			Steps: []Step{
				{Title: "Pick a preset or tune it by hand", Body: "A static preset pins the weather so it stays put. Dynamic lets it drift on its own.", Route: "weather"},
				{Title: "Choose how smoothly it changes", Body: "Transition speed controls how gradually weather moves: Smooth ramps over about half an hour in small steps, like vanilla. Fast swings the full range in two minutes, which is what makes weather feel like a light switch."},
				{Title: "Set the day length", Body: "Day and night acceleration multiply the passage of in-game time — 12 means a full day takes two real hours. Night acceleration is separate so you can keep nights short."},
			},
			Tips: []string{
				"Storm density only matters when there is enough rain and overcast for a storm to trigger at all.",
				"Snowfall only shows on snow-capable maps such as Sakhal.",
			},
		},
		{
			ID: "maintenance", Icon: "i-validator", Title: "Keeping it healthy",
			Intro: "Three things save servers: backups, the validator, and actually reading the logs when something breaks.",
			Image: "/img/validator.webp",
			Steps: []Step{
				{Title: "Turn on automatic backups", Body: "Settings can write a zip of your configs and mission files on a schedule and keep the newest N. Every single file the panel overwrites also gets a timestamped .bak next to it.", Route: "settings"},
				{Title: "Validate before you restart", Body: "The validator parses every mission XML, balances braces in .cfg files, checks that files listed in cfgeconomycore.xml exist and flags duplicate type names.", Route: "validator"},
				{Title: "Read the right log", Body: "The .RPT file records what the server itself did — a crash or a mod refusing to load shows up there. The .ADM file records what players did.", Route: "logs"},
			},
			Tips: []string{
				"Wipe deletes saved world state — players, vehicles, bases. Your loot tables and configs are untouched. Folders are moved aside first, so a mistaken wipe can be recovered by hand.",
				"Crash watchdog restarts the server if it exits on its own. After three crashes in five minutes it stops and shows a banner instead — a crash loop needs a human, not another restart.",
			},
		},
		{
			ID: "remote", Icon: "i-server", Title: "Access from a phone or another PC",
			Intro: "By default the panel only answers on this machine. Switching exposure to Local network makes it reachable from anything on the same Wi-Fi.",
			Image: "/img/settings.webp",
			Steps: []Step{
				{Title: "Switch exposure", Body: "Settings → Panel exposure → Local network, then restart the manager. The Network access card then lists the exact addresses to open.", Route: "settings"},
				{Title: "Allow it through the firewall", Body: "Windows asks once. If a phone cannot connect, allow the manager for Private networks in Windows Firewall."},
			},
			Tips: []string{
				"There is still no login. Never expose the panel straight to the internet — put Caddy or nginx with a password in front of it.",
			},
		},
	}
}

func ru() []Chapter {
	return []Chapter{
		{
			ID: "start", Icon: "i-dashboard", Title: "С чего начать",
			Intro: "Менеджер — это один .exe, который лежит рядом с сервером DayZ и заменяет Notepad и .bat-файлы веб-панелью. Он ничего не ставит в систему: удалил .exe — всё как было.",
			Image: "/img/dashboard.webp",
			Steps: []Step{
				{Title: "Положите exe в папку сервера", Body: "Скопируйте dayz-manager.exe рядом с DayZServer_x64.exe и serverDZ.cfg и запустите. Откроется консоль, а браузер перейдёт на http://127.0.0.1:8787."},
				{Title: "Укажите клиентский DayZ", Body: "При первом запуске спрашивается папка *клиента* DayZ — та установка игры, где лежит папка !Workshop. Именно туда Steam качает моды, и только оттуда менеджер может скопировать их на сервер.", Route: "settings"},
				{Title: "Запустите сервер", Body: "Нажмите «Запустить» на панели. Если DayZServer_x64.exe отсутствует или мод сломан, сервер выключится — причина будет в разделе «Логи».", Route: "dashboard"},
			},
			Tips: []string{
				"У панели нет логина. Держите режим «Только локально», если не доверяете сети; для доступа снаружи ставьте впереди reverse-proxy с паролем.",
				"Почти все редакторы файлов не сохраняют при запущенном сервере — DayZ держит блокировки на своих файлах. Остановите сервер; панель прямо пишет, когда причина в этом.",
			},
		},
		{
			ID: "mods", Icon: "i-mods", Title: "Моды",
			Intro: "Моды DayZ попадают в папку !Workshop клиента после подписки в Steam. Серверу нужна своя копия каждого мода, плюс его .bikey в папке keys, плюс сам мод в параметре запуска -mod. Менеджер делает все три шага.",
			Image: "/img/mods.webp",
			Steps: []Step{
				{Title: "Сначала подпишитесь в Steam", Body: "Менеджер копирует моды, а не качает их. Подпишитесь в Steam Workshop и дайте лаунчеру DayZ загрузить мод в !Workshop."},
				{Title: "Установите на сервер", Body: "Откройте «Моды» — там всё, что есть в !Workshop. «Установить» копирует мод в папку сервера и сам переносит .bikey. «Синхронизировать все» делает это для всего списка сразу.", Route: "mods"},
				{Title: "Включите для запуска", Body: "Установить и загрузить — разные вещи. Переключатель «Моды» в строке добавляет мод в -mod. Переключатель «Серверный» — только для модов, которых НЕ должно быть у клиентов (админ-инструменты)."},
				{Title: "Следите за порядком загрузки", Body: "Перетаскивайте строки так, чтобы фреймворки шли первыми: @CF раньше всего, что от него зависит, Expansion Core раньше своих аддонов. Неверный порядок — очень частая причина, почему сервер не стартует."},
			},
			Tips: []string{
				"«Обновить» копирует свежие файлы из !Workshop на сервер. Включите «Обновлять моды при каждом рестарте» в настройках, чтобы это происходило само.",
				"При удалении мода .bikey стирается только если тем же ключом не пользуется другой установленный мод — общие ключи (Expansion) остаются на месте.",
				"Если мод приносит лут, нажмите в его строке «Искать типы лута» — менеджер найдёт types.xml мода и зарегистрирует его.",
			},
		},
		{
			ID: "economy", Icon: "i-types", Title: "Лут и экономика",
			Intro: "Центральная экономика DayZ решает, что существует в мире и в каком количестве. types.xml — главный список: по записи на класс предмета, сколько его должно быть и где он может появляться. Неверные числа здесь — обычная причина, почему сервер кажется пустым или заваленным лутом.",
			Image: "/img/types.webp",
			Steps: []Step{
				{Title: "Разберитесь в четырёх числах", Body: "nominal — сколько предметов сервер старается держать в мире. min — порог, ниже которого запускается респавн. lifetime — сколько секунд нетронутый предмет живёт. restock — сколько сервер ждёт перед пополнением. nominal = 0 означает, что предмет не спавнится вообще.", Route: "types"},
				{Title: "Правьте прямо в таблице", Body: "nominal, min, lifetime и категория редактируются прямо в строке — изменённые строки подсвечены, пока не нажмёте «Сохранить изменения». Кнопка «Изменить» открывает полный редактор: usages, values, tags и flags."},
				{Title: "usage и value — это «где спавнится»", Body: "usage — тип здания (Military, Police, Farm…), value — тир лута (Tier1 — побережье, Tier4 — вглубь карты / военка). У предмета без usage нет места для спавна. Оба имени обязаны существовать в cfglimitsdefinition.xml."},
				{Title: "Используйте заготовки для массовой работы", Body: "Выделите строки и нажмите заготовку спавна — её usage/value/tags применятся ко всем сразу. Намного быстрее, чем править сотни записей руками."},
			},
			Tips: []string{
				"Свои предметы держите в отдельном файле своих types, а не в ванильном types.xml — менеджер сам пропишет его в cfgeconomycore.xml, и обновление игры его не затрёт.",
				"После массовых правок запускайте валидатор. Его авто-исправление вносит неизвестные usage/value/tag в cfglimitsdefinition.xml — именно после этого модовский лут начинает спавниться.",
			},
		},
		{
			ID: "attachments", Icon: "i-attach", Title: "Обвесы оружия",
			Intro: "cfgspawnabletypes.xml решает, с чем оружие появляется в мире: магазин, прицел, приклад. Файл состоит из слотов, и числа в нём означают две разные вещи — именно здесь все и ошибаются.",
			Image: "/img/attachments.webp",
			Steps: []Step{
				{Title: "Слот — это один бросок кубика", Body: "У каждого слота есть шанс: 1.0 — что-то появится всегда, 0.35 — в 35% случаев.", Route: "attachments"},
				{Title: "Предметы внутри слота конкурируют", Body: "Число у предмета — это относительный вес, а не проценты: DayZ выбирает РОВНО ОДИН предмет из слота. Веса 60 и 40 внутри слота 0.35 на деле означают 21% и 14%. Панель показывает эти реальные проценты, чтобы не считать вручную."},
				{Title: "Начните с шаблона", Body: "Выберите шаблон оружия (AKM, M4-A1, Mosin…) и подправьте. Классы автодополняются из вашего types.xml, а всё, чего там нет, подсвечивается — опечатка и есть обычная причина, почему обвес не появляется в игре."},
			},
			Tips: []string{
				"По слоту на каждую деталь: отдельный для магазинов, отдельный для прицелов, отдельный для приклада. Магазин и прицел в одном слоте означают «или то, или другое», но никогда оба.",
				"Переписывается только та запись, которую вы правите — ваши комментарии и форматирование в остальном файле сохраняются.",
			},
		},
		{
			ID: "rcon", Icon: "i-rcon", Title: "RCon, игроки и анонсы",
			Intro: "RCon — это удалённая консоль BattlEye: живой список игроков, кики, баны и сообщения в чат. Ей нужен пароль, который BattlEye читает из beserver_x64.cfg при старте сервера.",
			Image: "/img/players.webp",
			Steps: []Step{
				{Title: "Задайте пароль", Body: "Введите его в разделе RCon. Менеджер сам пропишет пароль в battleye/beserver_x64.cfg, создав файл при необходимости.", Route: "rcon"},
				{Title: "Перезапустите сервер", Body: "BattlEye читает этот файл только при запуске, поэтому новый пароль ничего не даст до следующего рестарта. Это нормально, а не баг."},
				{Title: "Анонсы и расписание", Body: "Анонсы по расписанию срабатывают в заданное время суток, по интервалу — каждые N минут. И те и другие идут через RCon, так что пароль должен работать.", Route: "settings"},
			},
			Tips: []string{
				"Предупреждения о рестарте тоже идут через RCon. Без пароля рестарт всё равно произойдёт — просто игроки не увидят отсчёт.",
				"Раздел «Игроки» строит историю из админ-лога: ники, GUID, наигранное время, убийства. Нужен флаг запуска -adminlog в настройках.",
			},
		},
		{
			ID: "weather", Icon: "i-weather", Title: "Погода и время",
			Intro: "Погода описана в cfgweather.xml миссии, который DayZ читает один раз при загрузке миссии — поэтому смена погоды всегда требует рестарта, и в ванили нельзя запланировать «дождь в 20:00» без мода.",
			Image: "/img/weather.webp",
			Steps: []Step{
				{Title: "Выберите пресет или настройте вручную", Body: "Статичный пресет фиксирует погоду, и она держится. «Динамическая» позволяет ей меняться самой.", Route: "weather"},
				{Title: "Выберите плавность перехода", Body: "«Плавность» задаёт, насколько постепенно меняется погода: «Плавно» — переход примерно за полчаса маленькими шагами, как в ванили. «Быстро» — весь диапазон за две минуты, отчего погода и ощущается как выключатель."},
				{Title: "Настройте длину суток", Body: "Ускорение дня и ночи умножает ход игрового времени: 12 означает, что полные сутки проходят за два реальных часа. Ночь настраивается отдельно, чтобы её можно было укоротить."},
			},
			Tips: []string{
				"Плотность грозы имеет смысл только тогда, когда дождя и облачности достаточно, чтобы гроза вообще началась.",
				"Снегопад виден только на снежных картах, например на Сахале.",
			},
		},
		{
			ID: "maintenance", Icon: "i-validator", Title: "Поддержание в порядке",
			Intro: "Сервер спасают три вещи: бэкапы, валидатор и привычка читать логи, когда что-то сломалось.",
			Image: "/img/validator.webp",
			Steps: []Step{
				{Title: "Включите авто-бэкапы", Body: "В настройках можно писать zip с конфигами и файлами миссии по расписанию и хранить N последних. Каждый файл, который панель перезаписывает, дополнительно получает рядом .bak с меткой времени.", Route: "settings"},
				{Title: "Проверяйте перед рестартом", Body: "Валидатор разбирает все XML миссии, сверяет скобки в .cfg, проверяет, что файлы из cfgeconomycore.xml существуют, и ловит дубликаты имён типов.", Route: "validator"},
				{Title: "Читайте нужный лог", Body: "Файл .RPT — что делал сам сервер: краш или мод, который не загрузился, видно там. Файл .ADM — что делали игроки.", Route: "logs"},
			},
			Tips: []string{
				"Вайп удаляет сохранённое состояние мира: игроков, машины, базы. Таблицы лута и конфиги не трогаются. Папки сначала переносятся в сторону, поэтому ошибочный вайп можно восстановить руками.",
				"Watchdog поднимает сервер, если он выключился сам. После трёх падений за пять минут он останавливается и показывает баннер: цикл падений требует человека, а не ещё одного рестарта.",
			},
		},
		{
			ID: "remote", Icon: "i-server", Title: "Доступ с телефона или другого ПК",
			Intro: "По умолчанию панель отвечает только на этой машине. Режим «Локальная сеть» делает её доступной со всего, что в той же Wi-Fi.",
			Image: "/img/settings.webp",
			Steps: []Step{
				{Title: "Смените режим доступа", Body: "Настройки → «Доступ к панели» → «Локальная сеть», затем перезапустите менеджер. После этого карточка «Доступ по сети» покажет точные адреса.", Route: "settings"},
				{Title: "Разрешите в брандмауэре", Body: "Windows спросит один раз. Если телефон не подключается — разрешите менеджер для частных сетей в брандмауэре Windows."},
			},
			Tips: []string{
				"Логина по-прежнему нет. Никогда не выставляйте панель напрямую в интернет — ставьте впереди Caddy или nginx с паролем.",
			},
		},
	}
}
