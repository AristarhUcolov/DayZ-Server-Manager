# Packs the two Windows builds into dayz-manager-windows.zip with a short
# readme, the Windows counterpart of dayz-manager-linux.tar.gz. Run after
# building (tools/build_release.py does both):
#
#   python tools/make_windows_zip.py
import os
import time
import zipfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FILES = ["dayz-manager-panel.exe", "dayz-manager-web.exe"]
README = """DayZ Server Manager for Windows
================================

1. Unpack this archive into your DayZ server folder (next to
   DayZServer_x64.exe).
2. Start dayz-manager-panel.exe: the panel opens in its own window and the
   manager stays in the tray. dayz-manager-web.exe is the same manager in a
   console window that opens the panel in your browser - use either one.
3. The first start asks whether to add the manager to the Start menu and the
   desktop. Settings -> General -> Startup starts it together with Windows.

The app window uses Microsoft WebView2 (built into Windows 10 and 11). Where
it is missing (Windows Server), the manager offers to install it and uses
the browser meanwhile.

Windows SmartScreen may say "Windows protected your PC" for a new download:
click "More info" -> "Run anyway".

---

DayZ Server Manager для Windows
===============================

1. Распакуйте архив в папку сервера DayZ (рядом с DayZServer_x64.exe).
2. Запустите dayz-manager-panel.exe: панель откроется в собственном окне,
   а менеджер останется в трее. dayz-manager-web.exe - тот же менеджер в
   окне консоли, открывающий панель в браузере; можно использовать любой.
3. При первом запуске менеджер предложит добавить себя в меню «Пуск» и на
   рабочий стол. Настройки -> Общие -> Автозапуск запускают его вместе с
   Windows.

Окно приложения использует Microsoft WebView2 (встроен в Windows 10 и 11).
Где его нет (Windows Server), менеджер предложит установить его, а пока
откроет панель в браузере.

Windows SmartScreen может написать «Система Windows защитила ваш
компьютер» для нового файла: нажмите «Подробнее» -> «Выполнить в любом
случае».
"""


def main():
    out = os.path.join(ROOT, "dayz-manager-windows.zip")
    stamp = time.localtime()[:6]
    with zipfile.ZipFile(out, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as z:
        for name in FILES:
            path = os.path.join(ROOT, name)
            if not os.path.exists(path):
                raise SystemExit("missing " + name + " - build it first")
            # A Linux binary under an .exe name (the build target was not
            # applied) would not start on Windows.
            with open(path, "rb") as f:
                if f.read(2) != b"MZ":
                    raise SystemExit(name + " is not a Windows program - rebuild it with tools/build_release.py")
            z.write(path, arcname=name)
        info = zipfile.ZipInfo("README-WINDOWS.txt", date_time=stamp)
        info.compress_type = zipfile.ZIP_DEFLATED
        # CRLF so Notepad on older Windows shows the lines.
        z.writestr(info, README.replace("\n", "\r\n").encode("utf-8-sig"))
    print("written", out, os.path.getsize(out), "bytes")


if __name__ == "__main__":
    main()
