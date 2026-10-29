# Changelog

**[English](#english) · [Русский](#русский) · [README](README.md)**

Every release of DayZ Server Manager, newest first. The panel shows the same
list under **What's new**; older versions are on the
[GitHub Releases](https://github.com/AristarhUcolov/DayZ-Server-Manager/releases) page.

---

# English

[Русская версия ↓](#русский)

## 1.1.0 — “The Halloween Update” (2026-10-29)

### Vehicles, attachments and presets — like the player loadout

- **Vehicles** works like the player loadout: a tab per vehicle, the car drawn
  from above with a tile per part (wheels, doors, hood, trunk lid, lights,
  battery, plugs, radiator), a tab per colour, and the settings beside it.
  A click on a part opens it in a dialog; empty slots add parts from the game's
  own list; “All colours” sets how complete every colour spawns; “Fill empty
  slots” puts the right part in each. **What's in the trunk** is edited the same
  way, and a weapon in the trunk gets attachments just like in the loadout: the
  “Attachments” button on its row edits the weapon's own entry, which DayZ uses
  wherever that weapon spawns. Boats, trucks and motorbikes have their own
  drawings.
- **Attachments** draws the item with its slots too, and every group — on the
  item and inside it — is a list of rows with the item's icon, name and class,
  its weight and real chance, added and swapped from the catalog instead of
  typing class names. Helmet visors and mouth guards light up on the drawing;
  slots it has no picture for show as dots under it.
- **Presets** edits its items with the same rows.

### Player loadout

- **Attachments no longer spawn ruined**: the panel saved the parts on a
  weapon or a vest without a condition, and DayZ made them ruined. Every item
  now has its own — pristine and full unless you choose otherwise — and
  loadouts saved earlier are fixed by themselves before the next server start
  (with a backup; the validator lists them too).
- **Hotbar keys for nested items**: a pistol in a vest's holster (or anything
  attached to clothing) can sit on a hotbar key — DayZ sets keys at any depth.
- **Loaded weapons**: a weapon with a magazine spawns loaded, a round in the
  chamber — nobody presses R. A Load button and “loaded / empty” marks show it.
- **Round in the chamber — your choice**: a switch under every loaded weapon.
  Off, the magazine goes in full and the player chambers a round with R. It
  applies to that weapon in every loadout, through the panel's server-side mod.
- **DayZ's attachment rules**: a bayonet and a suppressor or muzzle brake
  exclude each other, and a weapon light needs a handguard with a rail. The
  editor warns on the tile, offers to replace or to add the rail handguard, and
  saves parts in the order DayZ needs (handguard before its light).
- **Fresh character: food, water, warmth** — set how every new character
  starts (fed, watered, warmed up), applied by the panel's server-side mod.

### Stability and mods

- **The manager comes back by itself**: a small guardian starts it again (in
  the tray) if it stops without being closed — at most three times in
  15 minutes; the dashboard says so, with Windows' own crash record when there
  is one. It can be turned off in Settings → General.
- **Mods from !Workshop install again**: mods the DayZ Launcher links as
  junctions showed 0 B and failed with “copy mod: read …: Incorrect function”.
- **Steam Workshop download list**: remove rows, clear the list, and install a
  mod again after it was deleted from the server.
- **Choosing Steam Workshop as the mod source** no longer fails with
  “unknown mod source "steamcmd"”.

### Also new

- **Globals — “At a glance”**: sliders with pictures for the main numbers —
  infected and animals on the map, the condition loot spawns in, how far from
  players infected appear, how long a character stays after leaving, how often
  a base flag must be raised. The field below follows the slider.
- **Gameplay → Interface** is picked from pictures too: the hit indicator's
  look and behaviour (in the colour you set), the map (full screen or in the
  hands) and the glow around players.
- **Night lighting** on the Gameplay page is picked from four pictures of the
  same night, a candle showing how dark each one is.
- **Time of day** on the Server page is drawn: the sky at the start time, how
  long a day and a night last in real time.
- Neater forms: the loadout's first step and the weather smoothness row line up.
- All 23 languages updated.

### Earlier in 1.1

- **The manager no longer “closes by itself”**: most often that was Windows
  going to sleep. While a DayZ server runs the manager keeps the computer awake
  (Settings → General), remembers how it was closed and says so on the
  Dashboard; a crash writes the exact error to `crash.log`.
- **Steam Workshop as a mod source**: choose “Steam Workshop (direct
  download)” and update checks, “Install & update mods” and the update before
  restarts all go through SteamCMD.
- **Attachments in the player loadout**: the item is drawn with its slots
  (optics, muzzle, buttstock, magazine, a vest's pouches) offering the parts
  that fit according to the game files; characters are named; the page opens
  at once.
- **Weather**: the day animation can be stopped and the white marker dragged to
  any moment; manual weather is drawn as a scene with wind, rain, snow and fog.
- **Validator** in your language, with proper plural forms and a new auto-fix
  for repeated `cfgspawnabletypes.xml` entries.
- **Presets, Types, Attachments and Loot economy** share one style: tables,
  category colours and icons, chance bars, and a forecast when scaling loot.
- **Direct mod download from the Steam Workshop**: Mods → Direct download
  fetches mods straight onto the server with Valve's SteamCMD — no DayZ client
  and no download folder. Sign in once with a Steam account that owns DayZ (the
  password goes only to SteamCMD and is never stored; Steam Guard codes work).
  It also checks installed mods for updates.
- **New player loadout editor**: a survivor with every slot, an item picker
  built from the server's game files and mods, items in the hands and on the
  hotbar (1–0), condition, amount and contents per item, several kits.
- **Day & Night**: the sky, sunrise and sunset for the map, real-time day and
  night length including restarts, ready-made feels.
- **Vehicles**: drawings of the cars and fuel/coolant at spawn (a tiny
  server-side mod the panel generates; players need nothing).
- **Maps**: a ruler and shared admin markers on every map; the Heatmap adds
  building, raids and stashes from the admin log; the Live map shows trails.
- **Players**: message everyone, kick everyone and lock joining; CSV export;
  profiles list accounts from the same IP and their base-building history.
- **Phone access after the PC restarts** comes back by itself (the UPnP
  forward follows the new address; the public address is remembered and
  checked).
- **Player loadout without surprises**: “Enable” reconnects the loadout files
  after a server update (no more “fresh_spawn.json already exists”), the page
  shows unconnected files and broken entries, and the condition of carried
  items really applies.
- **After a DayZ server update** the manager puts back the RCon password, the
  mod-vehicle registration and the loadouts before the next start; the
  validator reports such cases with a Fix button.
- **Live map**: everyone's position every 5 minutes (the `adminLogPlayerList`
  switch), no logged-out or dead players, the map sized to the window.
- **Nasdara**: a drawing of the new map, its size and lighting; the Gameplay
  page knows the new DayZ 1.29 settings.
- **Panel access and HTTPS** apply at once, without restarting the manager.
- **Autostart** with Windows/Linux and starting the server with the manager;
  a ready systemd service for Linux without a desktop.
- **Mod vehicles** can be deleted; redesigned Types, Globals, Events and
  Presets pages; “Getting started” can be removed for good.
- **`dayz-manager-windows.zip`** — both Windows programs in one archive.

## 1.0.0 — “The October Update” (2026-10-14)

- **Multi-server**: several DayZ servers in one panel. The **Servers** page
  adds a server folder you have, or creates a new server as a copy of an
  existing one (free ports, a fresh world), and shows state, players, ports and
  port conflicts ("Assign free ports"). The server menu in the top bar switches
  the panel; each server keeps its own settings, mods, players, bans, chat,
  backups, schedule and Discord, while the login, remote access and language
  are shared.
- **Players**: tabs for all players (search, sorting), online now (ping, IP,
  GUID; message, kick, ban), bans (list with history, ban by Steam64 / ID /
  GUID / IP) and the kill feed; a profile with IDs, the current ban and the
  moderation history.
- **Bans that work**: from 1 hour to 30 days, permanent or custom, with a
  reason; the ban goes into the server's own `ban.txt` by player ID (takes
  effect at once) and into BattlEye by GUID; timed bans end automatically.
- **Discord**: embeds with the server's name, a choice of events (online,
  restarts with the reason, crashes, mods, backups, wipes, joins/leaves, PvP,
  chat, bans and kicks), a role ping on crashes, a separate player-activity
  channel, the bot's name/picture/language and a live status message.
- **Two builds** — a desktop app in its own window (`dayz-manager-panel`) and a
  browser build (`dayz-manager-web`), both for Windows and Linux.
- **Linux**: mod names lowercased automatically, Steam / SteamCMD / Flatpak /
  Snap libraries detected, the whole process group stopped cleanly.
- **Mod source of your choice**: the game client, any folder of `@mods` or
  SteamCMD's `workshop/content/221100`, or server-only (hosting without the game).
- **Restarts**: the countdown comes from the server (players and the panel see
  the same time), kick everyone N seconds before a restart and lock the server,
  your own warning text, a clean `#shutdown` that saves the world; restarting
  the manager picks the running server up again. CPU affinity and priority.
- **Real maps from your own mission**: terrain (`areaflags.map`), loot tiers,
  buildings, contaminated areas, territories; world size detected automatically.
- **Event spawn editor** with exact X / Y / Z (height included) and pasting a
  list of coordinates; new **Vehicles** and **Missions** pages. A map mission
  carried inside a mod (a folder or a .zip) is copied into `mpmissions`
  automatically when the mod is installed or updated; missions can also be
  imported from a .zip or a folder.
- **Validator 2.0** — grouped by problem, with a one-click fix for each.
- **RCon without "authentication failed"**: the panel logs in with the password
  BattlEye loaded at start (`beserver_x64_active_*.cfg`) and says plainly when
  a new password is waiting for a restart.
- **Pages rebuilt** — Events, Gameplay (readable names, descriptions and DayZ's
  default values), Vehicles, Weather & time, Loot economy, Types, Logs, Admin
  log, Players, Chat, the Discord settings (icons and a message preview) and
  more — after an audit from screenshots, with phone layouts.
- **Plurals in every language**: "1 point / 4 points", "1 точка / 4 точки / 5 точек".
- **The app feels installed**: on Windows, Start menu and desktop shortcuts and a
  window that remembers its size and place; on Linux, its own window (WebKitGTK).
- A getting-started checklist, "listed in the server browser" status via the
  Steam query, Types filtered by item kind, "Open folder" and Refresh buttons.

---

# Русский

[English version ↑](#english)

## 1.1.0 — «Хэллуинское обновление» (29.10.2026)

### Транспорт, обвесы и пресеты — как «Набор игрока»

- **Транспорт** устроен как «Набор игрока»: вкладка на каждую машину, машина
  нарисована сверху с плиткой на каждую деталь (колёса, двери, капот, крышка
  багажника, фары, аккумулятор, свечи, радиатор), вкладка на каждый цвет и
  настройки рядом. Клик по детали открывает её в окне; пустые слоты добавляют
  детали из списка самой игры; «Все цвета» задаёт комплектность всех цветов
  сразу; «Заполнить пустые слоты» ставит подходящую деталь в каждый. **Что в
  багажнике** редактируется так же, а оружию в багажнике обвесы задаются как в
  наборе игрока: кнопка «Обвесы» в его строке открывает собственную запись
  оружия, которую DayZ применяет везде, где это оружие появляется. У лодок,
  грузовиков и мотоциклов свои рисунки.
- **Обвесы** тоже рисуют предмет со слотами, а каждая группа — на предмете и
  внутри него — это строки с иконкой, названием и классом, весом и реальным
  шансом; вещи добавляются и заменяются из каталога, а не вводом классов.
  Визор и капа шлема подсвечиваются на рисунке; слоты без картинки
  показываются точками под ней.
- **Пресеты** редактируют свои предметы теми же строками.

### Набор игрока

- **Обвесы больше не появляются испорченными**: панель сохраняла детали на
  оружии и жилете без состояния, и DayZ создавал их испорченными. Теперь у
  каждой вещи своё состояние — новое и полное, если не выбрано другое, — а
  сохранённые раньше наборы исправляются сами перед следующим запуском сервера
  (с резервной копией; валидатор тоже их показывает).
- **Клавиши для вложенных вещей**: пистолет в кобуре бронежилета (и всё, что
  висит на одежде) можно назначить на клавишу — DayZ ставит их на любой
  глубине.
- **Заряженное оружие**: оружие с магазином появляется заряженным, с патроном
  в патроннике, — нажимать R не нужно. Кнопка «Зарядить» и отметки
  «заряжено / не заряжено».
- **Патрон в патроннике — на выбор**: переключатель у каждого заряженного
  оружия. Если выключить, магазин вставлен полным, а патрон игрок досылает сам
  (R). Действует для этого оружия во всех наборах, через серверный мод панели.
- **Правила обвесов DayZ**: штык-нож и глушитель или дульное устройство
  исключают друг друга, а фонарю нужно цевьё с планкой. Редактор предупреждает
  на плитке, предлагает заменить или поставить цевьё с планкой и сохраняет
  детали в нужном DayZ порядке (цевьё раньше фонаря).
- **Новый персонаж: сытость, вода, тепло** — с чем начинает каждый новый
  персонаж (сытым, без жажды, согретым); применяет серверный мод панели.

### Стабильность и моды

- **Менеджер поднимается сам**: небольшой сторож снова запускает его (в трее),
  если он остановился, не будучи закрытым, — не чаще трёх раз за 15 минут;
  панель сообщает об этом вместе с записью Windows о сбое, если она есть.
  Отключается в Настройках → Общие.
- **Моды из !Workshop снова устанавливаются**: моды, которые лаунчер DayZ
  связывает «junction»-ссылками, показывали 0 B и падали с ошибкой
  «copy mod: read …: Incorrect function».
- **Список загрузок Steam Workshop**: строки можно убрать, список — очистить,
  а удалённый с сервера мод — установить снова.
- **Выбор Steam Workshop источником модов** больше не даёт ошибку
  «unknown mod source "steamcmd"».

### Ещё

- **«Глобальные» — блок «Наглядно»**: ползунки с рисунками для главных чисел —
  заражённые и животные на карте, состояние появляющегося лута, на каком
  расстоянии от игроков появляются заражённые, сколько персонаж остаётся в мире
  после выхода, как часто поднимать флаг базы. Поле ниже меняется вместе с
  ползунком.
- **«Геймплей» → «Интерфейс»** тоже выбирается по картинкам: вид и поведение
  индикатора попаданий (в заданном цвете), карта (на весь экран или в руках) и
  свечение вокруг игрока.
- **Освещение ночью** в «Геймплее» выбирается по четырём картинкам одной и
  той же ночи — свеча показывает, насколько темно в каждом режиме.
- **Время суток** на странице «Сервер» нарисовано: небо в момент старта,
  длительность дня и ночи в реальном времени.
- Аккуратнее формы: первый шаг набора и строка плавности погоды выровнены.
- Обновлены все 23 языка.

### Ранее в 1.1

- **Менеджер больше не «закрывается сам»**: чаще всего это был спящий режим
  Windows. Пока работает сервер DayZ, менеджер не даёт компьютеру уснуть
  (Настройки → Общие), запоминает, как был закрыт, и показывает это на Панели;
  при сбое точная ошибка пишется в `crash.log`.
- **Steam Workshop как источник модов**: выберите «Steam Workshop (прямая
  загрузка)» — и проверка обновлений, «Установить и обновить моды» и
  автообновление перед рестартами работают через SteamCMD.
- **Обвесы в наборе игрока**: вещь нарисована, вокруг — слоты (прицел,
  дульное, приклад, магазин, подсумки разгрузки) с подходящими по файлам игры
  деталями; персонажи подписаны по именам; раздел открывается сразу.
- **Погода**: анимацию дня можно остановить, белую метку — тянуть к любому
  моменту; ручная погода нарисована сценой с ветром, дождём, снегом и туманом.
- **Валидатор** на вашем языке, с правильными формами чисел и новым
  авто-исправлением повторов в `cfgspawnabletypes.xml`.
- **Пресеты, Types, Обвесы и Экономика лута** — в едином стиле: таблицы,
  цвета и иконки категорий, шкалы шанса, прогноз при масштабировании лута.
- **Прямая загрузка модов из Steam Workshop**: Моды → Прямая загрузка
  скачивает моды прямо на сервер через SteamCMD от Valve — без клиента DayZ и
  без папки для загрузок. Достаточно один раз войти аккаунтом Steam, у которого
  есть DayZ (пароль уходит только в SteamCMD и нигде не хранится; поддерживаются
  коды Steam Guard). Там же проверка обновлений установленных модов.
- **Новый редактор набора игрока**: выживший со всеми слотами, выбор вещей из
  файлов игры и модов сервера, вещи в руках и на панели быстрого доступа
  (1–0), состояние, количество и содержимое каждой вещи, несколько наборов.
- **День и ночь**: небо, восход и закат для карты, длина дня и ночи в реальном
  времени с учётом рестартов, готовые варианты.
- **Транспорт**: рисунки машин и бензин/антифриз при появлении (маленький
  серверный мод, который создаёт панель; игрокам ничего не нужно).
- **Карты**: линейка и общие метки администраторов на всех картах; на тепловой
  карте — стройка, рейды и схроны из админ-лога; на живой — следы игроков.
- **Игроки**: сообщение всем, кик всех и закрытие входа; экспорт в CSV; в
  профиле — аккаунты с того же IP и история строительства.
- **Доступ с телефона после перезагрузки ПК** восстанавливается сам (UPnP
  переезжает на новый адрес, внешний адрес запоминается и проверяется).
- **Набор игрока без сюрпризов**: «Включить» снова подключает файлы наборов
  после обновления сервера (ошибки «fresh_spawn.json already exists» больше
  нет), страница показывает неподключённые файлы и сломанные записи, а
  состояние вещей в инвентаре теперь действительно применяется.
- **После обновления сервера DayZ** менеджер перед запуском сам возвращает
  пароль RCon, регистрацию транспорта из модов и наборы игрока; валидатор
  показывает такие случаи с кнопкой «Исправить».
- **Живая карта**: позиции всех игроков каждые 5 минут (переключатель
  `adminLogPlayerList`), без вышедших и погибших, карта по размеру окна.
- **Nasdara**: рисунок новой карты, её размер и освещение; страница Геймплей
  знает новые настройки DayZ 1.29.
- **Доступ к панели и HTTPS** применяются сразу, без перезапуска менеджера.
- **Автозапуск** вместе с Windows/Linux и запуск сервера вместе с менеджером;
  для Linux без рабочего стола — готовая служба systemd.
- **Транспорт из модов** можно удалить; обновлены разделы Types, Глобальные,
  События и Пресеты; «Первые шаги» можно убрать насовсем.
- **`dayz-manager-windows.zip`** — обе программы для Windows одним архивом.

## 1.0.0 — «Октябрьское обновление» (14.10.2026)

- **Мульти-сервер**: несколько серверов DayZ в одной панели. Страница
  **Серверы** добавляет существующую папку сервера или создаёт новый сервер
  копией существующего (со свободными портами и чистым миром), показывает
  состояние, игроков, порты и конфликты портов («Назначить свободные порты»).
  Меню сервера в верхней строке переключает панель; у каждого сервера свои
  настройки, моды, игроки, баны, чат, бэкапы, расписание и Discord, а вход,
  удалённый доступ и язык — общие.
- **Игроки**: вкладки «Все игроки» (поиск, сортировка), «Сейчас онлайн» (пинг,
  IP, GUID; сообщение, кик, бан), «Баны» (список с историей, бан по Steam64 /
  ID / GUID / IP) и килфид; профиль с ID, текущим баном и историей модерации.
- **Баны, которые работают**: срок от 1 часа до 30 дней, навсегда или свой,
  с причиной; бан пишется в собственный `ban.txt` сервера по ID игрока
  (действует сразу) и в BattlEye по GUID, временные снимаются автоматически.
- **Discord**: embed-сообщения с именем сервера, выбор событий (онлайн,
  рестарты с причиной, падения, моды, бэкапы, вайпы, входы/выходы, PvP,
  чат, баны и кики), пинг роли при падениях, отдельный канал для игроков,
  имя/картинка/язык бота и живое сообщение статуса.
- **Две сборки** — приложение в своём окне (`dayz-manager-panel`) и браузерная
  (`dayz-manager-web`), обе для Windows и Linux.
- **Linux**: автоматическое приведение имён модов к нижнему регистру, поиск
  Steam / SteamCMD / Flatpak / Snap, корректная остановка всей группы процессов.
- **Источник модов на выбор**: клиент игры, любая папка с `@модами` или
  `workshop/content/221100` от SteamCMD, либо «только сервер» (хостинг без игры).
- **Рестарты**: отсчёт берётся с сервера (игроки и панель видят одно время),
  кик всех за N секунд до рестарта с закрытием сервера, свой текст
  предупреждений, корректный `#shutdown` с сохранением мира; повторный запуск
  менеджера подхватывает уже работающий сервер. Привязка к ядрам CPU и приоритет.
- **Настоящие карты из вашей миссии**: рельеф (`areaflags.map`), тиры лута,
  постройки, заражённые зоны, территории; размер мира определяется сам.
- **Редактор точек спавна событий** с точными X / Y / Z (включая высоту) и
  вставкой списка координат; страницы **Транспорт** и **Миссии**. Миссия карты,
  лежащая внутри мода (папкой или .zip), сама копируется в `mpmissions` при
  установке/обновлении мода; импорт миссии из .zip или папки.
- **Вход в панель по логину и паролю** (по желанию — выбирается при первой
  настройке или в «Настройки → Доступ и безопасность»): пароли хранятся как солёные
  хеши PBKDF2, вход запоминается на устройстве, перебор паролей блокируется,
  список устройств со входом и выход с любого из них. Забыли пароль —
  запустите менеджер с `-reset-login`.
- **Удалённый доступ** («Настройки → Доступ и безопасность»): объясняет, почему
  телефон «уходит в оффлайн» вне дома, и проверяет каждый шаг — режим
  «Интернет» (только со входом), автоматический проброс портов на роутере
  (UPnP), правило брандмауэра Windows, HTTPS на том же порту, проверка белого
  IP / CGNAT с точным адресом для входа снаружи. Может пробросить и игровые порты.
- **Чат**: сообщения из менеджера (свои, личные, объявления, предупреждения о
  рестарте) видны в ленте, а каждый день сохраняется в архив с поиском и
  выгрузкой в .txt.
- **Страница «Сервер»**: миссии из `mpmissions` карточками карт (активная —
  в один клик), `serverDZ.cfg` по группам с переключателями, параметры запуска
  с ограничением FPS сервера (`-limitFPS`) и полем для любых дополнительных
  параметров DayZServer.
- **Настройки по вкладкам**: Общие, Доступ и безопасность, Рестарты и
  расписание, Бэкапы, Discord.
- **Модовый транспорт в «Транспорте»**: события из всех файлов, подключённых в
  `cfgeconomycore.xml`, распознавание машин по запчастям, сохранение в «свой»
  файл; транспорт, для которого мод приносит серверные файлы, добавляется в один
  клик (копируется в папку миссии `dzm_vehicles`).
- **Linux**: окно панели или браузер открываются и при запуске от root/sudo, со
  snap/flatpak-браузерами; в терминале — адрес панели, а на сервере без рабочего
  стола — инструкция (SSH-туннель) и флаги `-set-login`/`-exposure`.
- **FPS сервера на панели** — из строки «Average server FPS» в логе `.RPT`
  (интервал `logAverageFps` задаётся в группе «Лог сервера» на странице «Сервер»,
  нужен флаг `-dologs`).
- **Validator 2.0** — группировка по проблемам и исправление в один клик.
- **RCon без «authentication failed»**: панель входит с паролем, который BattlEye
  загрузил при старте (`beserver_x64_active_*.cfg`), и честно говорит, если
  новый пароль ждёт рестарта.
- **Переделаны страницы** «События», «Геймплей» (понятные названия, описания и
  значения DayZ по умолчанию), «Транспорт», «Погода и время», «Экономика лута»,
  «Types», «Логи», «Админ-лог», «Игроки», «Чат», настройки Discord (иконки и
  предпросмотр сообщений) и другие — по итогам аудита со скриншотами, с
  раскладкой для телефона.
- **Множественное число на всех языках**: «1 точка / 4 точки / 5 точек».
- **Приложение как программа**: в Windows — ярлыки в «Пуск» и на рабочем столе,
  окно запоминает размер и место; в Linux — собственное окно (WebKitGTK).
- Чек-лист «Первые шаги», статус «в списке серверов» через Steam-запрос,
  фильтр Types по типу предметов, кнопки «Открыть папку» и «Обновить».
