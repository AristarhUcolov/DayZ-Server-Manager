// Copyright (c) 2026 Aristarh Ucolov.
//
// Lightweight i18n bundle. Supported locales: "en", "ru".
// The web UI asks for the whole dictionary via /api/i18n so it can render
// without a round-trip per string.
package i18n

import "strings"

type Bundle map[string]string

// Returns the canonical locale name or empty for unknown.
func Name(code string) string {
	switch strings.ToLower(code) {
	case "en":
		return "English"
	case "ru":
		return "Русский"
	}
	return ""
}

func Supported() []string { return []string{"en", "ru"} }

// Get returns the dictionary for a locale, falling back to English.
func Get(code string) Bundle {
	switch strings.ToLower(code) {
	case "ru":
		return ru
	default:
		return en
	}
}

var en = Bundle{
	"app.name":                     "DayZ Server Manager",
	"app.author":                   "by Aristarh Ucolov",
	"app.copyright":                "© 2026 Aristarh Ucolov. All rights reserved.",

	"nav.dashboard":                "Dashboard",
	"nav.server":                   "Server",
	"nav.mods":                     "Mods",
	"nav.types":                    "Types",
	"nav.modedTypes":               "Custom types",
	"nav.files":                    "Files",
	"nav.logs":                     "Logs",
	"nav.events":                   "Events",
	"nav.rcon":                     "RCon",
	"nav.validator":                "Validator",
	"nav.settings":                 "Settings",

	"action.start":                 "Start server",
	"action.stop":                  "Stop server",
	"action.restart":                "Restart server",
	"action.save":                  "Save",
	"action.cancel":                "Cancel",
	"action.delete":                "Delete",
	"action.install":               "Install",
	"action.uninstall":             "Uninstall",
	"action.syncKeys":              "Sync keys",
	"action.apply":                 "Apply",
	"action.create":                "Create",
	"action.validate":              "Run validation",
	"action.openBrowser":           "Open in browser",
	"action.update":                "Update",
	"action.updateAll":             "Update all outdated",

	"status.running":                "Running",
	"status.stopped":                "Stopped",
	"status.uptime":                 "Uptime",
	"status.pid":                    "PID",
	"status.port":                   "Port",
	"status.players":                "Players",

	"firstRun.title":                "First-run setup",
	"firstRun.lang":                 "Language",
	"firstRun.vanillaPath":          "Path to your vanilla DayZ install (the client) — the one that owns the Steam !Workshop folder",
	"firstRun.vanillaPath.hint":     "Example: F:\\SteamLibrary\\steamapps\\common\\DayZ",
	"firstRun.exposure":             "Exposure",
	"firstRun.exposure.local":       "Local only (127.0.0.1)",
	"firstRun.exposure.internet":    "LAN / Internet (0.0.0.0)",
	"firstRun.adminUsername":        "Admin username",
	"firstRun.adminPassword":        "Admin password",
	"firstRun.adminPassword.hint":   "Required for LAN/Internet. Leave empty for local-only to skip login.",
	"firstRun.finish":               "Finish setup",

	"login.title":                   "Sign in",
	"login.username":                "Username",
	"login.password":                "Password",
	"login.submit":                  "Sign in",
	"login.invalid":                 "Invalid username or password.",
	"action.logout":                 "Sign out",

	"settings.title":                "Settings",
	"settings.language":             "Language",
	"settings.vanillaPath":          "Vanilla DayZ path",
	"settings.serverName":           "Server name",
	"settings.serverPort":           "Server port",
	"settings.serverCfg":            "Server config file",
	"settings.cpuCount":             "CPU count",
	"settings.bepath":               "BattlEye path",
	"settings.profilesDir":          "Profiles folder",
	"settings.flags":                "Launch flags",
	"settings.flag.dologs":          "Enable logs (-dologs)",
	"settings.flag.adminlog":        "Admin log (-adminlog)",
	"settings.flag.netlog":          "Network log (-netlog)",
	"settings.flag.freezecheck":     "Freeze check (-freezecheck)",
	"settings.flag.filePatching":    "File patching (-filePatching)",
	"settings.autorestart":          "Auto-restart interval (seconds)",
	"settings.autorestart.enable":   "Enable auto-restart",

	"mods.title":                    "Mods",
	"mods.installed":                "Installed in server",
	"mods.workshop":                 "Available in Workshop",
	"mods.keys":                     "keys",
	"mods.size":                     "size",
	"mods.updateAvailable":          "Update available",
	"mods.upToDate":                 "Up to date",
	"mods.updatedAt":                "Last modified",
	"mods.loadOrder":                "Load order",
	"mods.loadOrder.hint":           "Drag rows to reorder. The list is passed to DayZServer as -mod=@A;@B;@C so dependencies (frameworks like @CF) must come first.",

	"mission.duplicate":             "Duplicate",
	"mission.duplicate.prompt":      "Name of the new mission folder:",

	"types.title":                   "Types editor",
	"types.search":                  "Search by name",
	"types.field.nominal":           "Nominal",
	"types.field.min":               "Min",
	"types.field.lifetime":          "Lifetime",
	"types.field.restock":           "Restock",
	"types.field.quantmin":          "Quant min",
	"types.field.quantmax":          "Quant max",
	"types.field.cost":              "Cost",
	"types.field.category":          "Category",
	"types.field.usages":            "Usages",
	"types.field.values":            "Values",
	"types.field.tags":              "Tags",
	"types.field.flags":             "Flags",
	"types.presets":                 "Spawn presets",
	"types.presets.hint":            "Click a preset to merge its usage/value/tag fields into the selected types.",

	"moded.title":                   "Custom types (moded_types)",
	"moded.create":                  "Create new custom types file",
	"moded.fileName":                "File name (e.g. mymod_types.xml)",
	"moded.autoRegister":            "Automatically register in cfgeconomycore.xml",

	"events.title":                  "Events editor",
	"events.search":                 "Search by name",
	"events.field.nominal":          "Nominal",
	"events.field.min":              "Min",
	"events.field.max":              "Max",
	"events.field.lifetime":         "Lifetime",
	"events.field.restock":          "Restock",
	"events.field.saveable":         "Saveable",
	"events.field.active":           "Active",
	"events.field.children":         "Children",
	"events.child.type":             "Type",
	"events.child.min":              "Min",
	"events.child.max":              "Max",
	"events.child.lootmin":          "Loot min",
	"events.child.lootmax":          "Loot max",
	"events.addChild":               "Add child",
	"events.hint":                   "Edit spawn tables for zombies, vehicles, heli crashes and more.",

	"validator.title":                "Validator",
	"validator.none":                 "No issues found.",
	"validator.severity.error":       "Error",
	"validator.severity.warning":     "Warning",
	"validator.severity.info":        "Info",

	"guard.serverRunning":            "The server is running. Stop it before editing files.",

	"files.title":                    "Files",
	"files.tree":                     "Tree",
	"files.editor":                   "Editor",
	"files.save":                     "Save file",
}

var ru = Bundle{
	"app.name":                     "Менеджер сервера DayZ",
	"app.author":                   "автор: Аристарх Уколов",
	"app.copyright":                "© 2026 Аристарх Уколов. Все права защищены.",

	"nav.dashboard":                "Панель",
	"nav.server":                   "Сервер",
	"nav.mods":                     "Моды",
	"nav.types":                    "Types",
	"nav.modedTypes":               "Свои types",
	"nav.files":                    "Файлы",
	"nav.logs":                     "Логи",
	"nav.events":                   "События",
	"nav.rcon":                     "RCon",
	"nav.validator":                "Валидатор",
	"nav.settings":                 "Настройки",

	"action.start":                 "Запустить сервер",
	"action.stop":                  "Остановить сервер",
	"action.restart":                "Перезапустить",
	"action.save":                  "Сохранить",
	"action.cancel":                "Отмена",
	"action.delete":                "Удалить",
	"action.install":               "Установить",
	"action.uninstall":             "Удалить",
	"action.syncKeys":              "Синхр. ключи",
	"action.apply":                 "Применить",
	"action.create":                "Создать",
	"action.validate":              "Запустить проверку",
	"action.openBrowser":           "Открыть в браузере",
	"action.update":                "Обновить",
	"action.updateAll":             "Обновить все устаревшие",

	"status.running":                "Работает",
	"status.stopped":                "Остановлен",
	"status.uptime":                 "Аптайм",
	"status.pid":                    "PID",
	"status.port":                   "Порт",
	"status.players":                "Игроков",

	"firstRun.title":                "Первичная настройка",
	"firstRun.lang":                 "Язык",
	"firstRun.vanillaPath":          "Путь к обычному (клиентскому) DayZ — откуда брать моды из !Workshop",
	"firstRun.vanillaPath.hint":     "Пример: F:\\SteamLibrary\\steamapps\\common\\DayZ",
	"firstRun.exposure":             "Доступ",
	"firstRun.exposure.local":       "Только локально (127.0.0.1)",
	"firstRun.exposure.internet":    "LAN / Интернет (0.0.0.0)",
	"firstRun.adminUsername":        "Имя админа",
	"firstRun.adminPassword":        "Пароль админа",
	"firstRun.adminPassword.hint":   "Обязателен для LAN/Интернет. Оставьте пустым для локального режима без логина.",
	"firstRun.finish":               "Завершить настройку",

	"login.title":                   "Вход",
	"login.username":                "Имя пользователя",
	"login.password":                "Пароль",
	"login.submit":                  "Войти",
	"login.invalid":                 "Неверное имя или пароль.",
	"action.logout":                 "Выйти",

	"settings.title":                "Настройки",
	"settings.language":             "Язык",
	"settings.vanillaPath":          "Путь к vanilla DayZ",
	"settings.serverName":           "Название сервера",
	"settings.serverPort":           "Порт сервера",
	"settings.serverCfg":            "Файл конфигурации",
	"settings.cpuCount":             "Количество ядер CPU",
	"settings.bepath":               "Путь к BattlEye",
	"settings.profilesDir":          "Папка profiles",
	"settings.flags":                "Флаги запуска",
	"settings.flag.dologs":          "Логи (-dologs)",
	"settings.flag.adminlog":        "Admin-лог (-adminlog)",
	"settings.flag.netlog":          "Net-лог (-netlog)",
	"settings.flag.freezecheck":     "Freeze-check (-freezecheck)",
	"settings.flag.filePatching":    "File patching (-filePatching)",
	"settings.autorestart":          "Интервал авто-рестарта (секунд)",
	"settings.autorestart.enable":   "Включить авто-рестарт",

	"mods.title":                    "Моды",
	"mods.installed":                "Установлены на сервере",
	"mods.workshop":                 "Доступны в !Workshop",
	"mods.keys":                     "ключей",
	"mods.size":                     "размер",
	"mods.updateAvailable":          "Доступно обновление",
	"mods.upToDate":                 "Актуально",
	"mods.updatedAt":                "Изменён",
	"mods.loadOrder":                "Порядок загрузки",
	"mods.loadOrder.hint":           "Перетаскивайте строки. Список передаётся DayZServer как -mod=@A;@B;@C — зависимости (фреймворки вроде @CF) должны идти первыми.",

	"mission.duplicate":             "Дублировать",
	"mission.duplicate.prompt":      "Имя новой папки миссии:",

	"types.title":                   "Редактор types",
	"types.search":                  "Поиск по названию",
	"types.field.nominal":           "Nominal",
	"types.field.min":               "Min",
	"types.field.lifetime":          "Lifetime",
	"types.field.restock":           "Restock",
	"types.field.quantmin":          "Quant min",
	"types.field.quantmax":          "Quant max",
	"types.field.cost":              "Cost",
	"types.field.category":          "Категория",
	"types.field.usages":            "Usages",
	"types.field.values":            "Values",
	"types.field.tags":              "Tags",
	"types.field.flags":             "Flags",
	"types.presets":                 "Заготовки спавнов",
	"types.presets.hint":            "Нажмите на заготовку — её usage/value/tag объединятся с выбранными объектами.",

	"moded.title":                   "Свои types (moded_types)",
	"moded.create":                  "Создать новый файл своих types",
	"moded.fileName":                "Имя файла (например mymod_types.xml)",
	"moded.autoRegister":            "Автоматически зарегистрировать в cfgeconomycore.xml",

	"events.title":                  "Редактор событий",
	"events.search":                 "Поиск по имени",
	"events.field.nominal":          "Nominal",
	"events.field.min":              "Min",
	"events.field.max":              "Max",
	"events.field.lifetime":         "Lifetime",
	"events.field.restock":          "Restock",
	"events.field.saveable":         "Saveable",
	"events.field.active":           "Активно",
	"events.field.children":         "Дочерние",
	"events.child.type":             "Тип",
	"events.child.min":              "Min",
	"events.child.max":              "Max",
	"events.child.lootmin":          "Loot min",
	"events.child.lootmax":          "Loot max",
	"events.addChild":               "Добавить child",
	"events.hint":                   "Редактирование таблиц спавна зомби, машин, хеликрашей и т.д.",

	"validator.title":                "Валидатор",
	"validator.none":                 "Ошибок не найдено.",
	"validator.severity.error":       "Ошибка",
	"validator.severity.warning":     "Предупреждение",
	"validator.severity.info":        "Инфо",

	"guard.serverRunning":            "Сервер запущен. Остановите его перед редактированием файлов.",

	"files.title":                    "Файлы",
	"files.tree":                     "Дерево",
	"files.editor":                   "Редактор",
	"files.save":                     "Сохранить файл",
}
