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

## Что умеет

- **Заменяет стандартный .bat.** Запускает `DayZServer_x64.exe` с теми же
  параметрами (port, cpuCount, BEpath, profiles, -mod, dologs/adminlog/
  netlog/freezecheck, опционально filePatching) + опциональный авто-рестарт
  по интервалу.
- **Установка и обновление модов из Steam !Workshop.** Один раз указываешь
  путь к своему клиентскому DayZ — дальше в UI видишь все `@Моды`, которые
  Steam закачал. Жмёшь **Install** — мод копируется в папку сервера, а все
  `.bikey` автоматически переносятся в `keys/`. Жмёшь **Update** —
  перезаливается из Workshop (атомарно: новая версия ставится во временную
  папку и только потом подменяет старую, так что поломанное обновление не
  убивает сервер). Есть кнопка **Update all outdated** — обновляет одним
  кликом все моды, у которых в Workshop версия новее.
- **Безопасное удаление модов.** При удалении мода ключи из `keys/`
  удаляются только если **ни один другой установленный мод** не использует
  тот же `.bikey`. Shared-ключи типа `dayzexpansion.bikey` (CF/Core/AI
  делят его) остаются на месте, пока хоть один компонент стоит.
- **Редактор server.cfg в браузере.** Сохраняет комментарии и `class`-блоки
  round-trip (твой форматинг не ломается). Одним полем меняется карта
  (`template=`) — например `dayzOffline.chernarusplus` → `dayzOffline.enoch`.
- **Редактор types.xml.** Таблица с поиском, редактор каждого объекта:
  `nominal`, `min`, `lifetime`, `restock`, `quantmin/max`, `cost`,
  `category`, `flags`, `usages`, `values`, `tags`.
- **Заготовки спавнов (spawn presets).** Встроенный набор: Military Tier 3/4,
  Civilian, Industrial, Hunting, Rare. Выделяешь объекты → жмёшь заготовку →
  её `usage/value/tag` и настройки спавна применяются ко всем выбранным
  одним кликом.
- **Свои types (moded_types).** Создаёшь новый файл своих types в отдельной
  папке `mpmissions/<mission>/moded_types/`. Манагер **автоматически**
  вписывает его в `cfgeconomycore.xml`. Удалил файл — запись из
  `cfgeconomycore.xml` автоматически убирается.
- **Автопроверка файлов.** Сканирует все `.xml` под `mpmissions/` и
  балансирует скобки во всех `.cfg`, показывая файл и номер строки с
  ошибкой. Проверяет, что файлы из `cfgeconomycore.xml` реально
  существуют на диске, и подсвечивает дубликаты types между базовым
  `types.xml` и `moded_types/*.xml`.
- **Файл-менеджер.** Дерево файлов + редактор в браузере для любого
  `.xml`/`.cfg`/`.txt` внутри папки сервера.
- **Защита от повреждений.** Все write-операции возвращают 409 Conflict,
  если сервер запущен (DayZ держит блокировки на свои файлы). В UI сверху
  показывается баннер-предупреждение.
- **RU / EN интерфейс** с переключением на лету, плюс мастер первого
  запуска с выбором языка.
- **Локально или наружу.** По умолчанию слушает `127.0.0.1`. Флагом
  `-bind 0.0.0.0` открываешь доступ из LAN/интернета.

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

| флаг           | по умолчанию | назначение                                  |
|----------------|--------------|---------------------------------------------|
| `-port`        | `8787`       | Порт веб-панели.                            |
| `-bind`        | `127.0.0.1`  | Адрес привязки. `0.0.0.0` для доступа извне.|
| `-no-browser`  | `false`      | Не открывать браузер при запуске.           |
| `-version`     | —            | Напечатать версию и выйти.                  |

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
загрузки Windows. Пароль админа обязателен — включи его в мастере первого
запуска или в `manager.json` (`requireAuth: true`).

## Структура проекта

```
cmd/manager/             main.go — точка входа, CLI-флаги, авто-браузер
internal/app/            общий контекст приложения (конфиг, логгер, сервер)
internal/config/         manager.json и парсер server.cfg (round-trip)
internal/server/         контроллер процесса DayZServer_x64 + авто-рестарт
internal/mods/           скан Workshop, install/update/uninstall, sync-keys
internal/types/          types.xml + cfgeconomycore.xml, spawn presets
internal/validator/      XML/CFG + cross-file проверки
internal/i18n/           RU/EN строковые бандлы
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

## What it does

- **Replaces the stock .bat launcher.** Same launch parameters (port,
  cpuCount, BEpath, profiles, -mod, dologs/adminlog/netlog/freezecheck,
  optional filePatching) plus an optional auto-restart loop on a
  configurable interval.
- **Mod install & update from Steam !Workshop.** Point the manager at
  your client DayZ install once — the UI then lists every `@Mod` Steam
  has downloaded. Hit **Install**: the mod is copied into the server dir
  and every `.bikey` is auto-copied into `keys/`. Hit **Update**: it
  re-syncs from Workshop **atomically** (new version is staged in a temp
  dir and only then swapped in, so a broken update can't corrupt the
  server). An **Update all outdated** button updates every mod whose
  Workshop copy is newer in one click.
- **Smart uninstall.** When you remove a mod, keys in `keys/` are only
  deleted if **no other installed mod** provides the same `.bikey`.
  Shared signing keys (e.g. `dayzexpansion.bikey` used across CF /
  Core / AI) stay in place while any component is still installed.
- **server.cfg editor in the browser.** Preserves comments and `class`
  blocks on round-trip. One-field mission template changer — e.g. switch
  from `dayzOffline.chernarusplus` to `dayzOffline.enoch` instantly.
- **types.xml editor.** Searchable table + per-item editor for nominal,
  min, lifetime, restock, quantmin/max, cost, category, flags, usages,
  values, tags.
- **Spawn presets.** Built-in presets (Military Tier 3/4, Civilian,
  Industrial, Hunting, Rare). Select a set of types → click a preset →
  its usage/value/tag and spawn fields merge into all selected types at
  once.
- **Custom types (moded_types).** Create a new types file in a dedicated
  `mpmissions/<mission>/moded_types/` folder; the manager **auto-registers**
  it in `cfgeconomycore.xml`. Delete the file — the registration is
  auto-removed too.
- **Validator.** Scans every `.xml` under `mpmissions/` and checks brace
  balance in every `.cfg`, reporting file + line for syntax errors.
  Cross-checks that files referenced by `cfgeconomycore.xml` exist on
  disk and flags duplicate type names between the base `types.xml` and
  `moded_types/*.xml`.
- **File manager.** Tree view + in-browser editor for any `.xml`/`.cfg`/
  `.txt` inside the server directory.
- **Write-safety guard.** All file-writing endpoints return `409 Conflict`
  while the server is running (DayZ holds file locks on its working set).
  A warning banner is shown in the UI.
- **Bilingual UI (RU / EN)** with an on-the-fly language switcher and a
  language picker in the first-run wizard.
- **Local vs. Internet exposure.** Default bind is `127.0.0.1`. Use
  `-bind 0.0.0.0` to expose the panel on LAN / Internet.

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

| flag           | default       | meaning                                   |
|----------------|---------------|-------------------------------------------|
| `-port`        | `8787`        | Web panel port.                           |
| `-bind`        | `127.0.0.1`   | Bind address. Use `0.0.0.0` for LAN.      |
| `-no-browser`  | `false`       | Don't auto-open the browser on start.     |
| `-version`     | —             | Print version and exit.                   |

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
boots. **Always enable password auth** when exposing to LAN/Internet —
either through the first-run wizard or by setting `requireAuth: true`
in `manager.json`.

## REST API (summary)

- `GET  /api/info` — app info
- `GET  /api/i18n?lang=ru` — string bundle
- `GET  /api/config` · `POST /api/config` — manager config
- `POST /api/config/finish-first-run` — first-run wizard submission
- `GET  /api/server/status` — PID / uptime / port / running
- `POST /api/server/{start|stop|restart}`
- `GET  /api/servercfg` · `POST /api/servercfg` — read & write kv block
- `POST /api/servercfg/mission` — change mission template
- `GET  /api/mods` — Workshop + installed
- `POST /api/mods/{install|uninstall|update|update-all|sync-keys|enable}`
- `GET  /api/types?file=...` — list types
- `GET  /api/types/item?file=...&name=...` · `POST/DELETE` — per-item editor
- `GET  /api/types/presets` · `POST /api/types/apply-preset`
- `GET  /api/moded` · `POST /api/moded/{create|delete}`
- `GET  /api/files/tree?path=...` · `GET /api/files/read?path=...` · `POST /api/files/write`
- `GET  /api/validate`

All write endpoints return `409 Conflict` if the DayZ server is currently
running.

## License

Copyright © 2026 Aristarh Ucolov. All rights reserved. See
[LICENSE.md](LICENSE.md).
