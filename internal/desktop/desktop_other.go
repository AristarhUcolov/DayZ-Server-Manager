// Copyright (c) 2026 Aristarh Ucolov.
//go:build !windows

package desktop

import (
	"errors"
	"fmt"

	"dayzmanager/internal/launcher"
)

// Run shows the panel in an app-mode browser window (Chrome, Chromium, Edge,
// Brave… — also as flatpak, also when the manager runs as root through sudo).
// Without any such browser it opens the default browser; without a desktop
// session (a headless server) there is nothing to show: the manager keeps
// running and the start-up banner says how to open the panel from a PC.
func Run(inst *launcher.Instance, attachURL string) error {
	url := attachURL
	if inst != nil {
		url = inst.URL // printed — never with a sign-in token
	}
	if !launcher.HasDesktop() {
		if inst == nil {
			fmt.Println(text(inst, "desktop.headless") + " " + url)
			return nil
		}
		<-inst.Context().Done()
		return nil
	}
	// A real app window where the desktop has WebKitGTK; else an app-mode
	// browser window; else a browser tab.
	if err := runWebKit(inst, panelURL(inst, url)); err == nil {
		return nil
	} else if !errors.Is(err, errNoWebKit) {
		fmt.Println("app window:", err)
	}
	if runAppBrowser(inst, panelURL(inst, url)) {
		return nil
	}
	// No Chromium-family browser could open a window: the default browser tab
	// is the next best.
	fmt.Println(text(inst, "desktop.noAppBrowser") + " " + url)
	if !launcher.OpenBrowser(panelURL(inst, url)) {
		fmt.Println(text(inst, "launcher.banner.noBrowser"))
		if !launcher.IsTerminal() {
			// Started from a file manager or the menu: no terminal shows the
			// address, so say it in a window.
			go launcher.MessageBox(launcher.AppName, text(inst, "launcher.banner.noBrowser")+"\n\n"+url)
		}
	}
	if inst != nil {
		<-inst.Context().Done()
	}
	return nil
}

// Alert shows the message in a window where the desktop has a dialog tool
// (zenity, kdialog, …) and prints it: started from a file manager, nothing
// else would show an error.
func Alert(title, body string) {
	launcher.MessageBox(title, body)
}
