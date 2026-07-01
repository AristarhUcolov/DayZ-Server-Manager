# DayZ Server Manager

[![Go](https://img.shields.io/badge/go-1.22+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-Proprietary-orange)](LICENSE.md)
[![Author](https://img.shields.io/badge/author-Aristarh%20Ucolov-blue)](#)

**Author:** Aristarh Ucolov — © 2026. All rights reserved.

---

# RU

**DayZ Server Manager** — один exe-файл, который кидается в папку DayZ Server
и превращает обычный сервер в удобную панель управления в браузере. Никаких
`Notepad`, никаких `.bat` — всё через приятный веб-интерфейс. Вдохновлён
панелями хостингов GTA SA:MP.
<img width="1914" height="893" alt="{295FFA97-BA29-411B-8868-ECC23DE34220}" src="https://github.com/user-attachments/assets/bae4cc20-936a-47e8-8251-2e8948987e87" />

## Что умеет

- **Заменяет стандартный .bat.** Запускает `DayZServer_x64.exe` с теми же
  параметрами (port, cpuCount, BEpath, profiles, -mod, -serverMod,
  dologs/adminlog/netlog/freezecheck, опционально filePatching) + опциональный
  авто-рестарт по интервалу.
- **Установка и обновление модов из Steam !Workshop.** Один раз указываешь
  путь к своему клиентскому DayZ — дальше в UI видишь все `@Моды`, которые
  Steam закачал. Жмёшь **Install** — мод копируется в папку сервера, а все
  `.bikey` автоматически переносятся в `keys/`. Жмёшь **Update** —
  перезаливается из Workshop (атомарно: новая версия ставится во временную
  папку и только потом подменяет старую, так что поломанное обновление не
  убивает сервер). Есть **Update all outdated** и **Sync all** — привести
  папку сервера в соответствие с `!Workshop` одним кликом.
- **Авто-обновление модов.** Можно обновлять моды перед каждым рестартом и
  периодически проверять `!Workshop` — если подписанный мод там новее,
  сервер обновится и перезапустится с обычным обратным отсчётом.
- **Порядок загрузки и серверные моды.** Порядок `-mod=` меняется
  перетаскиванием (зависимости вроде `@CF` — первыми). Отдельный тумблер для
  серверных модов (`-serverMod=`).
- **Безопасное удаление модов.** При удалении мода ключи из `keys/`
  удаляются только если **ни один другой установленный мод** не использует
  тот же `.bikey`. Shared-ключи типа `dayzexpansion.bikey` (CF/Core/AI
  делят его) остаются на месте, пока хоть один компонент стоит.
- **Редактор server.cfg в браузере.** Сохраняет комментарии и `class`-блоки
  round-trip (твой форматинг не ломается). Одним полем меняется карта
  (`template=`) — например `dayzOffline.chernarusplus` → `dayzOffline.enoch`.
- **Редактор types.xml.** Таблица с поиском, инлайн-правка `nominal`, `min`,
  `lifetime`, `category` прямо в таблице + полный редактор каждого объекта:
  `restock`, `quantmin/max`, `cost`, `flags`, `usages`, `values`, `tags`.
- **Заготовки спавнов (spawn presets).** Встроенный набор: Military Tier 3/4,
  Civilian, Industrial, Hunting, Rare. Выделяешь объекты → жмёшь заготовку →
  её `usage/value/tag` и настройки спавна применяются ко всем выбранным.
- **Редактор событий (events).** Таблицы спавна зомби, машин, хеликрашей:
  `nominal/min/max/lifetime/restock`, дочерние элементы.
- **Свои types (moded_types).** Создаёшь новый файл своих types; манагер
  **автоматически** вписывает его в `cfgeconomycore.xml`. Можно
  импортировать `*_types.xml` прямо из установленного мода.
- **Валидатор с авто-исправлением.** Сканирует все `.xml` под `mpmissions/`,
  балансирует скобки в `.cfg`, проверяет существование файлов из
  `cfgeconomycore.xml`, ловит дубликаты types. **Авто-исправление** вносит
  неизвестные `usage/value/tag/category` в `cfglimitsdefinition.xml` (с
  бэкапом) — чинит модовский лут, который DayZ иначе молча выкидывает.
- **RCon.** Список игроков, kick/ban, broadcast в чат, произвольная команда.
  Пароль RCon задаётся прямо в панели — манагер пишет его в
  `battleye/beserver_x64.cfg` (создаёт файл при необходимости).
- **Анонсы и рестарты.** Анонсы по расписанию (в заданное время суток) и по
  интервалу (каждые N минут), ежедневные рестарты по HH:MM с обратным
  отсчётом-предупреждением по RCon.
- **Погода и время.** Пресеты, ручная настройка каждого канала, ускорение
  дня/ночи и стартовое время (`serverTime`).
- **Вайп сервера.** Очистка сохранённого состояния мира (игроки, машины,
  базы, территории) — папки персистенса сначала перемещаются в
  `.dayz-manager/wipes/<метка>/`, ошибочный вайп можно восстановить.
- **Импорт существующего сервера.** Указываешь чужую папку сервера — манагер
  показывает моды/миссию/serverDZ.cfg и даёт перенять что нужно.
- **Логи и админ-лог.** Просмотр `.RPT`/`.ADM` с хвостовым стримом; разбор
  событий админ-лога (коннекты, убийства, чат) с фильтрами.
- **Бэкап/восстановление.** Скачать zip с `manager.json`, `serverDZ.cfg`,
  BE-конфигами и файлами миссии — или восстановить из него. Каждая перезапись
  DayZ-файла делает `.bak` (хранятся 5 последних).
- **Автопроверка/защита от повреждений.** Все write-операции возвращают
  409 Conflict, если сервер запущен (DayZ держит блокировки на свои файлы).
  В UI сверху показывается баннер-предупреждение.
- **11 языков интерфейса** с переключением на лету: English, Русский,
  Español, Français, Deutsch, Italiano, Português, Moldovenească, 中文,
  日本語, 한국어. Мастер первого запуска с выбором языка.
- **Локально, LAN или наружу.** По умолчанию слушает `127.0.0.1`. Режим
  доступа выбирается в настройках; для доступа с телефона/других устройств
  показываются готовые адреса. Встроенного логина нет — LAN-режим только в
  доверенной сети или за reverse-proxy с авторизацией.

## Установка и запуск

Требуется Go 1.22+ для сборки. (Готовый бинарник скачивается из
GitHub Releases — см. ниже.)

```bash
# из корня проекта
go build -o dayz-manager.exe ./cmd/manager
```

Кросс-сборка под Windows из Linux/macOS:

```bash
GOOS=windows GOARCH=amd64 go build -o dayz-manager.exe ./cmd/manager
```

Готовый exe полностью самодостаточен — весь веб-интерфейс встроен в бинарь
через `//go:embed`. Никакого Node.js, никаких зависимостей.

### Как использовать

1. Скопируй `dayz-manager.exe` в папку DayZ Server (рядом с
   `DayZServer_x64.exe` и `serverDZ.cfg`).
2. Запусти двойным кликом — появится консоль, браузер откроет
   `http://127.0.0.1:8787/`.
3. На первом запуске укажи путь к своему клиентскому DayZ (где папка
   `!Workshop`), выбери язык и режим доступа (`Local` / `LAN/Internet`).
4. Пользуйся панелью. Перед редактированием файлов **останавливай сервер**.

### Флаги командной строки

| флаг           | по умолчанию | назначение                                          |
|----------------|--------------|-----------------------------------------------------|
| `-port`        | `8787`       | Порт веб-панели.                                    |
| `-bind`        | *(из настроек)* | Адрес привязки. Пусто = следует режиму доступа: `127.0.0.1` для Local, `0.0.0.0` для LAN. |
| `-no-browser`  | `false`      | Не открывать браузер при запуске.                   |
| `-version`     | —            | Напечатать версию и выйти.                          |

### Запуск как Windows Service (NSSM)

Чтобы панель стартовала с системой и автоматически перезапускалась при
падениях, удобнее всего использовать [NSSM](https://nssm.cc/):

```bat
nssm install DayZManager "C:\DayZServer\dayz-manager.exe"
nssm set DayZManager AppDirectory "C:\DayZServer"
nssm set DayZManager AppParameters "-bind 0.0.0.0 -no-browser"
nssm set DayZManager Start SERVICE_AUTO_START
nssm start DayZManager
```

После этого панель будет доступна по `http://<сервер>:8787/` сразу после
загрузки Windows. У панели нет встроенного логина — если открываешь её
наружу, ставь впереди reverse-proxy (Caddy / nginx) с HTTP Basic auth.

## Структура проекта

```
cmd/manager/             main.go — точка входа, CLI-флаги, авто-браузер
internal/app/            общий контекст приложения (конфиг, логгер, сервер, RCon)
internal/config/         manager.json, парсер server.cfg (round-trip), beserver_x64.cfg
internal/server/         контроллер процесса DayZServer_x64 + расписания/авто-рестарт
internal/mods/           скан Workshop, install/update/uninstall, sync-keys
internal/types/          types.xml + cfgeconomycore.xml, events, spawn presets
internal/validator/      XML/CFG + cross-file проверки + авто-исправление лимитов
internal/weather/        cfgweather.xml — пресеты и ручная погода
internal/rcon/           BattlEye RCon-клиент (UDP) + менеджер соединения
internal/logs/           обнаружение и хвостовой стрим .RPT/.ADM
internal/admlog/          разбор событий админ-лога (.ADM)
internal/updater/        проверка обновлений манагера
internal/util/           бэкапы, диск, вспомогательное
internal/i18n/           строковые бандлы (11 языков), по файлу на локаль
internal/web/            HTTP-сервер, REST API, встроенные статические файлы
internal/web/static/     index.html, app.css, app.js (встраиваются при сборке)
```

## Лицензия

© 2026 Аристарх Уколов. Все права защищены. См. [LICENSE.md](LICENSE.md).

---

# ENG

**DayZ Server Manager** is a single-exe web panel for managing a DayZ
dedicated server. Drop it into your DayZ Server folder and a browser-based
admin panel opens. No more Notepad, no more fiddling with `.bat` files —
everything through a clean web UI. Inspired by classic GTA SA:MP hosting
panels.
<img width="1912" height="897" alt="{6C2F5542-BC13-48A3-9B8E-670671B97C7C}" src="https://github.com/user-attachments/assets/408af898-ad66-4de2-b176-f0b703d6dcf1" />

## What it does

- **Replaces the stock .bat launcher.** Same launch parameters (port,
  cpuCount, BEpath, profiles, -mod, -serverMod, dologs/adminlog/netlog/
  freezecheck, optional filePatching) plus an optional auto-restart loop on
  a configurable interval.
- **Mod install & update from Steam !Workshop.** Point the manager at
  your client DayZ install once — the UI then lists every `@Mod` Steam
  has downloaded. Hit **Install**: the mod is copied into the server dir
  and every `.bikey` is auto-copied into `keys/`. Hit **Update**: it
  re-syncs from Workshop **atomically** (new version is staged in a temp
  dir and only then swapped in, so a broken update can't corrupt the
  server). **Update all outdated** and **Sync all** bring the server dir in
  line with `!Workshop` in one click.
- **Mod auto-update.** Optionally refresh mods before every restart and
  poll `!Workshop` periodically — when a subscribed mod is newer there, the
  server updates and restarts with the usual countdown.
- **Load order & server-side mods.** The `-mod=` order is drag-to-reorder
  (dependencies like `@CF` first). A separate toggle marks server-only mods
  (`-serverMod=`).
- **Smart uninstall.** When you remove a mod, keys in `keys/` are only
  deleted if **no other installed mod** provides the same `.bikey`.
  Shared signing keys (e.g. `dayzexpansion.bikey` used across CF /
  Core / AI) stay in place while any component is still installed.
- **server.cfg editor in the browser.** Preserves comments and `class`
  blocks on round-trip. One-field mission template changer — e.g. switch
  from `dayzOffline.chernarusplus` to `dayzOffline.enoch` instantly.
- **types.xml editor.** Searchable table with inline nominal / min /
  lifetime / category editing, plus a per-item editor for restock,
  quantmin/max, cost, flags, usages, values, tags.
- **Spawn presets.** Built-in presets (Military Tier 3/4, Civilian,
  Industrial, Hunting, Rare). Select a set of types → click a preset →
  its usage/value/tag and spawn fields merge into all selected types.
- **Events editor.** Spawn tables for zombies, vehicles and heli crashes:
  nominal/min/max/lifetime/restock plus child spawns.
- **Custom types (moded_types).** Create a new types file; the manager
  **auto-registers** it in `cfgeconomycore.xml`. You can also import
  `*_types.xml` straight out of an installed mod.
- **Validator with auto-fix.** Scans every `.xml` under `mpmissions/`,
  checks brace balance in `.cfg`, verifies files referenced by
  `cfgeconomycore.xml` exist, flags duplicate types. **Auto-fix**
  whitelists unknown `usage/value/tag/category` into
  `cfglimitsdefinition.xml` (with a `.bak`) — the fix for modded loot DayZ
  otherwise silently drops.
- **RCon.** Player list, kick/ban, chat broadcast, raw command. Set the
  RCon password right in the panel — the manager writes it into
  `battleye/beserver_x64.cfg` (creating the file if needed).
- **Announcements & restarts.** Scheduled announcements (at a daily time)
  and interval announcements (every N minutes), plus scheduled daily
  restarts at HH:MM with an RCon countdown warning.
- **Weather & time.** Presets, a manual per-channel editor, day/night time
  acceleration and start time (`serverTime`).
- **Server wipe.** Clears saved world state (players, vehicles, bases,
  territories) — persistence folders are first moved to
  `.dayz-manager/wipes/<timestamp>/`, so a mistaken wipe can be restored.
- **Import an existing server.** Point at another server folder — the
  manager previews its mods / mission / serverDZ.cfg and lets you absorb
  what you want.
- **Logs & admin log.** View `.RPT`/`.ADM` with a tailing stream; parse
  admin-log events (connects, kills, chat) with filters.
- **Backup / restore.** Download a zip of `manager.json`, `serverDZ.cfg`,
  BE configs and mission files — or restore from one. Every overwrite of a
  DayZ file keeps a `.bak` (5 most recent).
- **Write-safety guard.** All file-writing endpoints return `409 Conflict`
  while the server is running (DayZ holds file locks on its working set).
  A warning banner is shown in the UI.
- **11-language UI** with an on-the-fly switcher: English, Русский, Español,
  Français, Deutsch, Italiano, Português, Moldovenească, 中文, 日本語, 한국어.
  A language picker in the first-run wizard.
- **Local, LAN or Internet exposure.** Default bind is `127.0.0.1`.
  Exposure is chosen in Settings; reachable URLs for phones/other devices
  are shown for you. There is no built-in login — use LAN mode only on a
  trusted network, or front it with an authenticating reverse proxy.

## Build

Requires Go 1.22+. (Pre-built binaries are on the GitHub Releases page.)

```bash
# from the project root
go build -o dayz-manager.exe ./cmd/manager
```

Cross-compiling to Windows from Linux/macOS:

```bash
GOOS=windows GOARCH=amd64 go build -o dayz-manager.exe ./cmd/manager
```

The resulting binary is fully self-contained — the web UI is embedded via
`//go:embed`. No Node.js, no runtime deps.

### Usage

1. Copy `dayz-manager.exe` into your DayZ Server folder
   (next to `DayZServer_x64.exe` and `serverDZ.cfg`).
2. Double-click it. A console window opens and your browser launches
   `http://127.0.0.1:8787/`.
3. On first run, enter the path to your *client* DayZ install (the one
   with the `!Workshop/` folder), pick a language, and choose `Local` or
   `LAN/Internet` exposure.
4. Use the panel. **Stop the server** before editing files.

### Command-line flags

| flag           | default          | meaning                                             |
|----------------|------------------|-----------------------------------------------------|
| `-port`        | `8787`           | Web panel port.                                     |
| `-bind`        | *(from settings)* | Bind address. Blank = follows exposure: `127.0.0.1` for Local, `0.0.0.0` for LAN. |
| `-no-browser`  | `false`          | Don't auto-open the browser on start.               |
| `-version`     | —                | Print version and exit.                             |

### Running as a Windows Service (NSSM)

For unattended hosting, run the panel as a service with
[NSSM](https://nssm.cc/):

```bat
nssm install DayZManager "C:\DayZServer\dayz-manager.exe"
nssm set DayZManager AppDirectory "C:\DayZServer"
nssm set DayZManager AppParameters "-bind 0.0.0.0 -no-browser"
nssm set DayZManager Start SERVICE_AUTO_START
nssm start DayZManager
```

The panel will be reachable at `http://<server>:8787/` right after Windows
boots. The panel has **no built-in login** — when exposing it to LAN /
Internet, put Caddy / nginx with HTTP Basic auth in front.

## REST API (summary)

- `GET  /api/info` — app info
- `GET  /api/i18n` — string bundle + language list
- `GET  /api/config` · `POST /api/config` — manager config
- `POST /api/config/finish-first-run` — first-run wizard submission
- `GET  /api/server/status` — PID / uptime / port / running
- `POST /api/server/{start|stop|restart}`
- `GET  /api/servercfg` · `POST /api/servercfg` — read & write kv block
- `POST /api/servercfg/mission` — change mission template
- `GET  /api/mods` — Workshop + installed
- `POST /api/mods/{install|uninstall|update|update-all|sync-all|sync-keys|enable|order}`
- `GET  /api/types?file=...` · `GET /api/types/item` · `POST /api/types/bulk-patch`
- `GET  /api/types/presets` · `POST /api/types/apply-preset`
- `GET  /api/events` · `GET /api/moded` · `POST /api/moded/{create|delete}`
- `GET  /api/validate` · `POST /api/validate/fix`
- `GET  /api/rcon/players` · `POST /api/rcon/{say|kick|ban|command}`
- `GET  /api/weather` · `POST /api/weather/{preset|custom|time}`
- `GET  /api/wipe/preview` · `POST /api/wipe`
- `GET  /api/logs/{list|read|stream}` · `GET /api/admlog/recent`
- `GET  /api/backup/export` · `POST /api/backup/import`
- `GET  /api/files/tree?path=...` · `GET /api/files/read?path=...` · `POST /api/files/write`

All write endpoints return `409 Conflict` if the DayZ server is currently
running.

## License

Copyright © 2026 Aristarh Ucolov. All rights reserved. See
[LICENSE.md](LICENSE.md).
