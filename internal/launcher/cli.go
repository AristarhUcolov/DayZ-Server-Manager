// Copyright (c) 2026 Aristarh Ucolov.

package launcher

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"dayzmanager/internal/app"
	"dayzmanager/internal/config"
	"dayzmanager/internal/i18n"
	"dayzmanager/internal/web"
)

// applyExposure saves a -exposure override before the panel starts.
func applyExposure(a *app.App, exposure string) error {
	exposure = strings.ToLower(strings.TrimSpace(exposure))
	if exposure == "" {
		return nil
	}
	switch exposure {
	case "local", "lan", "internet":
	default:
		return fmt.Errorf("-exposure must be local, lan or internet (got %q)", exposure)
	}
	if exposure == "internet" && !web.LoginConfigured(a.ManagerDir) {
		return errors.New("-exposure internet needs the panel login: add -set-login <name> (you will be asked for a password)")
	}
	if err := a.MutateConfig(func(c *config.Manager) error { c.Exposure = exposure; return nil }); err != nil {
		return err
	}
	a.Log.Printf("panel exposure set to %q from the command line", exposure)
	return nil
}

// SetLoginInteractive turns the panel login on for user, asking for the
// password on the terminal (twice, not echoed where the terminal allows).
func SetLoginInteractive(user string) error {
	dir, err := ServerDir()
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	p1, err := readSecret(in, "Password for "+user+": ")
	if err != nil {
		return err
	}
	p2, err := readSecret(in, "Repeat the password: ")
	if err != nil {
		return err
	}
	if p1 != p2 {
		return errors.New("the passwords do not match")
	}
	if err := web.SetLoginCredentials(filepath.Join(dir, ".dayz-manager"), user, p1); err != nil {
		return err
	}
	fmt.Printf("Panel login turned on for %q.\n", strings.TrimSpace(user))
	return nil
}

// readSecret reads one line without echoing it when stdin is a terminal
// (stty on Unix; Windows consoles echo — the flag is meant for headless Linux).
func readSecret(in *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	restore := func() {}
	if runtime.GOOS != "windows" {
		if p, err := exec.LookPath("stty"); err == nil {
			off := exec.Command(p, "-echo")
			off.Stdin = os.Stdin
			if off.Run() == nil {
				restore = func() {
					on := exec.Command(p, "echo")
					on.Stdin = os.Stdin
					_ = on.Run()
					fmt.Println()
				}
			}
		}
	}
	line, err := in.ReadString('\n')
	restore()
	if err != nil && line == "" {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// text returns a launcher string in the manager's language, with {name}
// placeholders filled in.
func (i *Instance) text(key string, pairs ...string) string {
	s, ok := i18n.Get(i.App.Cfg().Language)[key]
	if !ok {
		s = i18n.Get("en")[key]
	}
	for k := 0; k+1 < len(pairs); k += 2 {
		s = strings.ReplaceAll(s, "{"+pairs[k]+"}", pairs[k+1])
	}
	return s
}

// virtualIface reports container and VM bridges (Docker, libvirt, LXC, CNI):
// their addresses are internal to this computer, no other device reaches them.
func virtualIface(name string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "lxc", "lxd", "cni", "flannel", "podman"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// sshHost picks the host for the banner's ssh command: the address the
// current SSH login connected to (sshConn is SSH_CONNECTION), else the first
// public address, else the first one.
func sshHost(sshConn string, ips []string) string {
	if f := strings.Fields(sshConn); len(f) >= 3 {
		if ip := net.ParseIP(f[2]); ip != nil && !ip.IsLoopback() {
			return ip.String()
		}
	}
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil && !ip.IsPrivate() {
			return s
		}
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return ""
}

// shellQuote quotes a path for a shell command line when it needs it.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t'\"$`\\!*?;&|<>()[]{}#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// lanAddresses lists this computer's IPv4 addresses other devices can use
// (private ones first).
func lanAddresses() []string {
	var priv, other []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || virtualIface(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil || n.IP.IsLinkLocalUnicast() {
				continue
			}
			if n.IP.IsPrivate() {
				priv = append(priv, n.IP.String())
			} else {
				other = append(other, n.IP.String())
			}
		}
	}
	sort.Strings(priv)
	sort.Strings(other)
	return append(priv, other...)
}

// Banner is the block printed to the terminal once the panel is up: where to
// open it, from other devices too, and — on a machine without a desktop — how
// to reach it from a PC. browserOpened=false adds a note that no browser could
// be started here.
func (i *Instance) Banner(browserOpened bool) string {
	line := strings.Repeat("-", 66)
	var b strings.Builder
	b.WriteString("\n" + line + "\n")
	app := i.text("app.name")
	if app == "" {
		app = AppName
	}
	b.WriteString("  " + i.text("launcher.banner.running", "app", app, "version", i.App.Version) + "\n\n")
	b.WriteString("  " + i.text("launcher.banner.panel") + "  " + i.URL + "\n")
	if i.bind == "" {
		for _, ip := range lanAddresses() {
			b.WriteString("  " + i.text("launcher.banner.lan") + "  http://" + ip + ":" + strconv.Itoa(i.port) + "/\n")
		}
	}
	if !hasDesktop() {
		name := os.Getenv("USER")
		if u, err := user.Current(); err == nil && u.Username != "" {
			name = u.Username
		}
		if os.Geteuid() == 0 {
			// started with sudo: root usually may not log in over SSH
			if s := sudoUser(); s != "" {
				name = s
			}
		}
		if name == "" {
			name = "user"
		}
		host := sshHost(sshConnection(), lanAddresses())
		if host == "" {
			host, _ = os.Hostname()
		}
		if host == "" {
			host = "server"
		}
		b.WriteString("\n  " + strings.ReplaceAll(i.text("launcher.banner.headless",
			"port", strconv.Itoa(i.port), "user", name, "host", host), "\n", "\n  ") + "\n")
	} else if !browserOpened {
		b.WriteString("\n  " + i.text("launcher.banner.noBrowser") + "\n")
	}
	if runtime.GOOS == "linux" && hasDesktop() && !desktopInstalled() {
		// The applications menu is how a desktop user expects to start it.
		exe, _ := os.Executable()
		b.WriteString("\n  " + i.text("launcher.banner.install", "cmd", shellQuote(exe)) + "\n")
	}
	b.WriteString("\n  " + i.text("launcher.banner.keep") + "\n" + line + "\n")
	return b.String()
}
