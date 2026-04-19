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
	"firstRun.finish":               "Finish setup",

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
	"firstRun.finish":               "Завершить настройку",

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
