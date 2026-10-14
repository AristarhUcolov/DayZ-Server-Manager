# Packs the two Linux builds into dayz-manager-linux.tar.gz with the
# executable bit set — a browser download loses it, which is the usual reason
# "the file does not open" on a Linux desktop. Run after building:
#
#   GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dayz-manager-web-linux ./cmd/manager
#   GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dayz-manager-panel-linux ./cmd/panel
#   python tools/make_linux_tarball.py
import io
import os
import tarfile
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FILES = ["dayz-manager-panel-linux", "dayz-manager-web-linux"]
README = """DayZ Server Manager for Linux
==============================

1. Unpack this archive into your DayZ server folder (next to the DayZServer
   binary). Both programs in it are already executable.
2. With a desktop, run once in a terminal in that folder:

       ./dayz-manager-panel-linux -install

   The manager is then in your applications menu (and on the desktop) - start
   it from there like any other app. -uninstall removes the shortcuts.
3. On a server without a desktop (a VPS over SSH):

       ./dayz-manager-web-linux -no-browser

   The terminal shows how to open the panel from your PC (an SSH tunnel).

If a downloaded copy "does not open": the browser removed its permission to
run - `chmod +x dayz-manager-*-linux` - and many file managers never start
programs on a double click; use -install as above.

---

DayZ Server Manager для Linux
=============================

1. Распакуйте архив в папку сервера DayZ (рядом с бинарником DayZServer).
   Обе программы в нём уже исполняемые.
2. С рабочим столом один раз выполните в терминале в этой папке:

       ./dayz-manager-panel-linux -install

   После этого менеджер есть в меню приложений (и на рабочем столе) -
   запускайте его оттуда, как любое приложение. -uninstall убирает ярлыки.
3. На сервере без рабочего стола (VPS по SSH):

       ./dayz-manager-web-linux -no-browser

   В терминале будет написано, как открыть панель с вашего ПК (SSH-туннель).

Если скачанный файл «не открывается»: браузер снял с него право на запуск -
`chmod +x dayz-manager-*-linux` - а многие файловые менеджеры вообще не
запускают программы двойным щелчком; используйте -install, как выше.
"""


def main():
    out = os.path.join(ROOT, "dayz-manager-linux.tar.gz")
    now = int(time.time())
    with tarfile.open(out, "w:gz") as tar:
        for name in FILES:
            path = os.path.join(ROOT, name)
            if not os.path.exists(path):
                raise SystemExit("missing " + name + " - build it first")
            # A Windows .exe under a -linux name (the build target was not
            # applied) is exactly what made the Linux builds "not start".
            with open(path, "rb") as f:
                if f.read(4) != b"\x7fELF":
                    raise SystemExit(name + " is not a Linux program - rebuild it with tools/build_release.py")
            info = tar.gettarinfo(path, arcname=name)
            info.mode = 0o755
            info.uid = info.gid = 0
            info.uname = info.gname = ""
            with open(path, "rb") as f:
                tar.addfile(info, f)
        data = README.encode("utf-8")
        info = tarfile.TarInfo("README-LINUX.txt")
        info.size, info.mode, info.mtime = len(data), 0o644, now
        tar.addfile(info, io.BytesIO(data))
    print("written", out, os.path.getsize(out), "bytes")


if __name__ == "__main__":
    main()
