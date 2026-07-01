// Copyright (c) 2026 Aristarh Ucolov.
//
// Additional UI locales. These translate the CORE, constantly-visible strings
// (navigation, actions, statuses); every other key falls back to English via
// Get's merge, so the UI is never broken — it just shows English for the long,
// rarely-seen hints until a native speaker fills them in.
package i18n

// es — Español
var es = Bundle{
	"app.author": "por Aristarh Ucolov",
	"nav.dashboard": "Panel", "nav.server": "Servidor", "nav.mods": "Mods", "nav.types": "Types",
	"nav.modedTypes": "Types personalizados", "nav.missionDb": "BD de misión", "nav.files": "Archivos",
	"nav.logs": "Registros", "nav.events": "Eventos", "nav.rcon": "RCon", "nav.battleye": "BattlEye",
	"nav.validator": "Validador", "nav.sync": "Importar", "nav.profiles": "Perfiles de mods",
	"nav.weather": "Clima y hora", "nav.wipe": "Wipe", "nav.settings": "Ajustes", "nav.admlog": "Registro admin",
	"nav.group.server": "Servidor", "nav.group.economy": "Economía", "nav.group.config": "Configuración",
	"nav.group.live": "En vivo", "nav.group.system": "Sistema",
	"action.start": "Iniciar servidor", "action.stop": "Detener servidor", "action.restart": "Reiniciar servidor",
	"action.save": "Guardar", "action.cancel": "Cancelar", "action.delete": "Eliminar", "action.edit": "Editar",
	"action.install": "Instalar", "action.uninstall": "Desinstalar", "action.update": "Actualizar",
	"action.updateAll": "Actualizar todo", "action.reload": "Recargar", "action.create": "Crear",
	"action.validate": "Validar", "action.syncKeys": "Sincronizar claves",
	"status.running": "En ejecución", "status.stopped": "Detenido", "status.uptime": "Tiempo activo",
	"status.pid": "PID", "status.port": "Puerto", "status.players": "Jugadores",
	"settings.saved": "Guardado ✓", "cmdp.search": "Buscar",
}

// fr — Français
var fr = Bundle{
	"app.author": "par Aristarh Ucolov",
	"nav.dashboard": "Tableau de bord", "nav.server": "Serveur", "nav.mods": "Mods", "nav.types": "Types",
	"nav.modedTypes": "Types personnalisés", "nav.missionDb": "BD de mission", "nav.files": "Fichiers",
	"nav.logs": "Journaux", "nav.events": "Événements", "nav.rcon": "RCon", "nav.battleye": "BattlEye",
	"nav.validator": "Validateur", "nav.sync": "Importer", "nav.profiles": "Profils de mods",
	"nav.weather": "Météo et heure", "nav.wipe": "Wipe", "nav.settings": "Paramètres", "nav.admlog": "Journal admin",
	"nav.group.server": "Serveur", "nav.group.economy": "Économie", "nav.group.config": "Configuration",
	"nav.group.live": "En direct", "nav.group.system": "Système",
	"action.start": "Démarrer le serveur", "action.stop": "Arrêter le serveur", "action.restart": "Redémarrer le serveur",
	"action.save": "Enregistrer", "action.cancel": "Annuler", "action.delete": "Supprimer", "action.edit": "Modifier",
	"action.install": "Installer", "action.uninstall": "Désinstaller", "action.update": "Mettre à jour",
	"action.updateAll": "Tout mettre à jour", "action.reload": "Recharger", "action.create": "Créer",
	"action.validate": "Valider", "action.syncKeys": "Synchroniser les clés",
	"status.running": "En cours", "status.stopped": "Arrêté", "status.uptime": "Disponibilité",
	"status.pid": "PID", "status.port": "Port", "status.players": "Joueurs",
	"settings.saved": "Enregistré ✓", "cmdp.search": "Rechercher",
}

// de — Deutsch
var de = Bundle{
	"app.author": "von Aristarh Ucolov",
	"nav.dashboard": "Übersicht", "nav.server": "Server", "nav.mods": "Mods", "nav.types": "Types",
	"nav.modedTypes": "Eigene Types", "nav.missionDb": "Missions-DB", "nav.files": "Dateien",
	"nav.logs": "Protokolle", "nav.events": "Events", "nav.rcon": "RCon", "nav.battleye": "BattlEye",
	"nav.validator": "Validator", "nav.sync": "Import", "nav.profiles": "Mod-Profile",
	"nav.weather": "Wetter & Zeit", "nav.wipe": "Wipe", "nav.settings": "Einstellungen", "nav.admlog": "Admin-Log",
	"nav.group.server": "Server", "nav.group.economy": "Wirtschaft", "nav.group.config": "Konfiguration",
	"nav.group.live": "Live", "nav.group.system": "System",
	"action.start": "Server starten", "action.stop": "Server stoppen", "action.restart": "Server neu starten",
	"action.save": "Speichern", "action.cancel": "Abbrechen", "action.delete": "Löschen", "action.edit": "Bearbeiten",
	"action.install": "Installieren", "action.uninstall": "Deinstallieren", "action.update": "Aktualisieren",
	"action.updateAll": "Alle aktualisieren", "action.reload": "Neu laden", "action.create": "Erstellen",
	"action.validate": "Prüfen", "action.syncKeys": "Schlüssel synchronisieren",
	"status.running": "Läuft", "status.stopped": "Gestoppt", "status.uptime": "Laufzeit",
	"status.pid": "PID", "status.port": "Port", "status.players": "Spieler",
	"settings.saved": "Gespeichert ✓", "cmdp.search": "Suchen",
}

// it — Italiano
var it = Bundle{
	"app.author": "di Aristarh Ucolov",
	"nav.dashboard": "Pannello", "nav.server": "Server", "nav.mods": "Mods", "nav.types": "Types",
	"nav.modedTypes": "Types personalizzati", "nav.missionDb": "DB missione", "nav.files": "File",
	"nav.logs": "Log", "nav.events": "Eventi", "nav.rcon": "RCon", "nav.battleye": "BattlEye",
	"nav.validator": "Validatore", "nav.sync": "Importa", "nav.profiles": "Profili mod",
	"nav.weather": "Meteo e ora", "nav.wipe": "Wipe", "nav.settings": "Impostazioni", "nav.admlog": "Log admin",
	"nav.group.server": "Server", "nav.group.economy": "Economia", "nav.group.config": "Configurazione",
	"nav.group.live": "In diretta", "nav.group.system": "Sistema",
	"action.start": "Avvia server", "action.stop": "Ferma server", "action.restart": "Riavvia server",
	"action.save": "Salva", "action.cancel": "Annulla", "action.delete": "Elimina", "action.edit": "Modifica",
	"action.install": "Installa", "action.uninstall": "Disinstalla", "action.update": "Aggiorna",
	"action.updateAll": "Aggiorna tutto", "action.reload": "Ricarica", "action.create": "Crea",
	"action.validate": "Valida", "action.syncKeys": "Sincronizza chiavi",
	"status.running": "In esecuzione", "status.stopped": "Fermato", "status.uptime": "Tempo attivo",
	"status.pid": "PID", "status.port": "Porta", "status.players": "Giocatori",
	"settings.saved": "Salvato ✓", "cmdp.search": "Cerca",
}

// pt — Português
var pt = Bundle{
	"app.author": "por Aristarh Ucolov",
	"nav.dashboard": "Painel", "nav.server": "Servidor", "nav.mods": "Mods", "nav.types": "Types",
	"nav.modedTypes": "Types personalizados", "nav.missionDb": "BD da missão", "nav.files": "Arquivos",
	"nav.logs": "Registros", "nav.events": "Eventos", "nav.rcon": "RCon", "nav.battleye": "BattlEye",
	"nav.validator": "Validador", "nav.sync": "Importar", "nav.profiles": "Perfis de mods",
	"nav.weather": "Clima e hora", "nav.wipe": "Wipe", "nav.settings": "Configurações", "nav.admlog": "Log de admin",
	"nav.group.server": "Servidor", "nav.group.economy": "Economia", "nav.group.config": "Configuração",
	"nav.group.live": "Ao vivo", "nav.group.system": "Sistema",
	"action.start": "Iniciar servidor", "action.stop": "Parar servidor", "action.restart": "Reiniciar servidor",
	"action.save": "Salvar", "action.cancel": "Cancelar", "action.delete": "Excluir", "action.edit": "Editar",
	"action.install": "Instalar", "action.uninstall": "Desinstalar", "action.update": "Atualizar",
	"action.updateAll": "Atualizar tudo", "action.reload": "Recarregar", "action.create": "Criar",
	"action.validate": "Validar", "action.syncKeys": "Sincronizar chaves",
	"status.running": "Em execução", "status.stopped": "Parado", "status.uptime": "Tempo ativo",
	"status.pid": "PID", "status.port": "Porta", "status.players": "Jogadores",
	"settings.saved": "Salvo ✓", "cmdp.search": "Pesquisar",
}

// ro — Moldovenească / Română
var ro = Bundle{
	"app.author": "de Aristarh Ucolov",
	"nav.dashboard": "Panou", "nav.server": "Server", "nav.mods": "Moduri", "nav.types": "Types",
	"nav.modedTypes": "Types personalizate", "nav.missionDb": "BD misiune", "nav.files": "Fișiere",
	"nav.logs": "Jurnale", "nav.events": "Evenimente", "nav.rcon": "RCon", "nav.battleye": "BattlEye",
	"nav.validator": "Validator", "nav.sync": "Import", "nav.profiles": "Profiluri de moduri",
	"nav.weather": "Vreme și timp", "nav.wipe": "Wipe", "nav.settings": "Setări", "nav.admlog": "Jurnal admin",
	"nav.group.server": "Server", "nav.group.economy": "Economie", "nav.group.config": "Configurare",
	"nav.group.live": "Live", "nav.group.system": "Sistem",
	"action.start": "Pornește serverul", "action.stop": "Oprește serverul", "action.restart": "Repornește serverul",
	"action.save": "Salvează", "action.cancel": "Anulează", "action.delete": "Șterge", "action.edit": "Editează",
	"action.install": "Instalează", "action.uninstall": "Dezinstalează", "action.update": "Actualizează",
	"action.updateAll": "Actualizează tot", "action.reload": "Reîncarcă", "action.create": "Creează",
	"action.validate": "Validează", "action.syncKeys": "Sincronizează cheile",
	"status.running": "În rulare", "status.stopped": "Oprit", "status.uptime": "Timp activ",
	"status.pid": "PID", "status.port": "Port", "status.players": "Jucători",
	"settings.saved": "Salvat ✓", "cmdp.search": "Caută",
}

// zh — 中文 (简体)
var zh = Bundle{
	"app.author": "作者 Aristarh Ucolov",
	"nav.dashboard": "仪表盘", "nav.server": "服务器", "nav.mods": "模组", "nav.types": "Types",
	"nav.modedTypes": "自定义 Types", "nav.missionDb": "任务数据库", "nav.files": "文件",
	"nav.logs": "日志", "nav.events": "事件", "nav.rcon": "RCon", "nav.battleye": "BattlEye",
	"nav.validator": "校验器", "nav.sync": "导入", "nav.profiles": "模组配置",
	"nav.weather": "天气与时间", "nav.wipe": "清档", "nav.settings": "设置", "nav.admlog": "管理日志",
	"nav.group.server": "服务器", "nav.group.economy": "经济", "nav.group.config": "配置",
	"nav.group.live": "实时", "nav.group.system": "系统",
	"action.start": "启动服务器", "action.stop": "停止服务器", "action.restart": "重启服务器",
	"action.save": "保存", "action.cancel": "取消", "action.delete": "删除", "action.edit": "编辑",
	"action.install": "安装", "action.uninstall": "卸载", "action.update": "更新",
	"action.updateAll": "全部更新", "action.reload": "重新加载", "action.create": "创建",
	"action.validate": "校验", "action.syncKeys": "同步密钥",
	"status.running": "运行中", "status.stopped": "已停止", "status.uptime": "运行时间",
	"status.pid": "PID", "status.port": "端口", "status.players": "玩家",
	"settings.saved": "已保存 ✓", "cmdp.search": "搜索",
}

// ja — 日本語
var ja = Bundle{
	"app.author": "作者 Aristarh Ucolov",
	"nav.dashboard": "ダッシュボード", "nav.server": "サーバー", "nav.mods": "Mod", "nav.types": "Types",
	"nav.modedTypes": "カスタム Types", "nav.missionDb": "ミッションDB", "nav.files": "ファイル",
	"nav.logs": "ログ", "nav.events": "イベント", "nav.rcon": "RCon", "nav.battleye": "BattlEye",
	"nav.validator": "バリデーター", "nav.sync": "インポート", "nav.profiles": "Modプロファイル",
	"nav.weather": "天候と時間", "nav.wipe": "ワイプ", "nav.settings": "設定", "nav.admlog": "管理ログ",
	"nav.group.server": "サーバー", "nav.group.economy": "経済", "nav.group.config": "設定",
	"nav.group.live": "ライブ", "nav.group.system": "システム",
	"action.start": "サーバー起動", "action.stop": "サーバー停止", "action.restart": "サーバー再起動",
	"action.save": "保存", "action.cancel": "キャンセル", "action.delete": "削除", "action.edit": "編集",
	"action.install": "インストール", "action.uninstall": "アンインストール", "action.update": "更新",
	"action.updateAll": "すべて更新", "action.reload": "再読み込み", "action.create": "作成",
	"action.validate": "検証", "action.syncKeys": "キーを同期",
	"status.running": "稼働中", "status.stopped": "停止", "status.uptime": "稼働時間",
	"status.pid": "PID", "status.port": "ポート", "status.players": "プレイヤー",
	"settings.saved": "保存しました ✓", "cmdp.search": "検索",
}

// ko — 한국어
var ko = Bundle{
	"app.author": "제작 Aristarh Ucolov",
	"nav.dashboard": "대시보드", "nav.server": "서버", "nav.mods": "모드", "nav.types": "Types",
	"nav.modedTypes": "사용자 Types", "nav.missionDb": "미션 DB", "nav.files": "파일",
	"nav.logs": "로그", "nav.events": "이벤트", "nav.rcon": "RCon", "nav.battleye": "BattlEye",
	"nav.validator": "검사기", "nav.sync": "가져오기", "nav.profiles": "모드 프로필",
	"nav.weather": "날씨 및 시간", "nav.wipe": "초기화", "nav.settings": "설정", "nav.admlog": "관리자 로그",
	"nav.group.server": "서버", "nav.group.economy": "경제", "nav.group.config": "구성",
	"nav.group.live": "실시간", "nav.group.system": "시스템",
	"action.start": "서버 시작", "action.stop": "서버 중지", "action.restart": "서버 재시작",
	"action.save": "저장", "action.cancel": "취소", "action.delete": "삭제", "action.edit": "편집",
	"action.install": "설치", "action.uninstall": "제거", "action.update": "업데이트",
	"action.updateAll": "전체 업데이트", "action.reload": "새로고침", "action.create": "생성",
	"action.validate": "검사", "action.syncKeys": "키 동기화",
	"status.running": "실행 중", "status.stopped": "중지됨", "status.uptime": "가동 시간",
	"status.pid": "PID", "status.port": "포트", "status.players": "플레이어",
	"settings.saved": "저장됨 ✓", "cmdp.search": "검색",
}
