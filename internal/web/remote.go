// Copyright (c) 2026 Aristarh Ucolov.
//
// Remote access helpers (Settings → Remote access). "The panel works on my
// phone at home but goes offline at work" is almost never a manager bug: the
// phone at home talks to the PC's LAN address, which does not exist outside
// the home network. Reaching it from the internet needs (1) the panel open to
// the internet (exposure "internet", which requires a login), (2) a port
// forward on the router, (3) the firewall letting it in, and (4) a public IP —
// many providers put homes behind CGNAT, where no forward can work and a
// tunnel/VPN (Tailscale, ZeroTier, Radmin VPN, Cloudflare Tunnel) is the only
// way. This file checks each of those and fixes what can be fixed from here:
// UPnP port forwards, a Windows Firewall rule, and a CGNAT diagnosis.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/config"
	"dayzmanager/internal/upnp"
	"dayzmanager/internal/util"
)

const upnpDesc = "DayZ Server Manager"

type portState struct {
	Proto   string `json:"proto"`
	Port    int    `json:"port"`
	Purpose string `json:"purpose"` // panel | game | query
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

type upnpState struct {
	mu         sync.Mutex
	LastRun    int64       `json:"lastRun"`
	Running    bool        `json:"running"`
	Router     string      `json:"router"`
	LocalIP    string      `json:"localIp"`
	ExternalIP string      `json:"externalIp"`
	Public     *bool       `json:"public"`
	Error      string      `json:"error"`
	Ports      []portState `json:"ports"`
	again      bool        // a change arrived while a sync was running: run once more
	synced     string      // portsKey of the forwards the last sync worked towards
}

func (s *upnpState) view() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]interface{}{"lastRun": s.LastRun, "running": s.Running, "router": s.Router,
		"localIp": s.LocalIP, "externalIp": s.ExternalIP, "public": s.Public, "error": s.Error,
		"ports": append([]portState(nil), s.Ports...)}
}

func (h *handlers) registerRemote(mux *http.ServeMux) {
	mux.HandleFunc("/api/remote/status", methods(h.remoteStatus, http.MethodGet))
	mux.HandleFunc("/api/remote/upnp", methods(h.remoteUPnP, http.MethodPost))
	mux.HandleFunc("/api/remote/check", methods(h.remoteCheck, http.MethodPost))
	mux.HandleFunc("/api/remote/firewall", methods(h.remoteFirewall, http.MethodPost))
}

// wantedPorts lists the forwards the current settings ask for.
func (h *handlers) wantedPorts(cfg config.Manager) []portState {
	var out []portState
	if cfg.UPnPPanel && h.panelPort > 0 {
		out = append(out, portState{Proto: "TCP", Port: h.panelPort, Purpose: "panel"})
	}
	if cfg.UPnPGame {
		// Every server of the panel: the setting is panel-wide.
		if h.server != nil && h.server.fleet != nil {
			seen := map[string]bool{}
			for _, sh := range h.server.handlersList() {
				for _, p := range sh.gamePorts(sh.app.Cfg()) {
					if k := portKey(p); !seen[k] {
						seen[k] = true
						out = append(out, p)
					}
				}
			}
		} else {
			out = append(out, h.gamePorts(cfg)...)
		}
	}
	return out
}

// gamePorts are what DayZ players need: the game port and the three after it
// (UDP), and the Steam query port. The RCon port is never forwarded.
func (h *handlers) gamePorts(cfg config.Manager) []portState {
	base := cfg.ServerPort
	if base <= 0 {
		base = 2302
	}
	var out []portState
	seen := map[int]bool{}
	for p := base; p <= base+3; p++ {
		if p == cfg.RConPort {
			continue
		}
		seen[p] = true
		out = append(out, portState{Proto: "UDP", Port: p, Purpose: "game"})
	}
	if q := h.app.SteamQueryPort(); q > 0 && !seen[q] && q != cfg.RConPort {
		out = append(out, portState{Proto: "UDP", Port: q, Purpose: "query"})
	}
	return out
}

// mappedFile remembers which forwards this manager created, so turning the
// option off (even after a restart) removes them from the router again.
func (h *handlers) mappedFile() string { return filepath.Join(h.app.ManagerDir, "upnp.json") }

func (h *handlers) loadMapped() []portState {
	var list []portState
	if data, err := os.ReadFile(h.mappedFile()); err == nil {
		_ = json.Unmarshal(data, &list)
	}
	return list
}

func (h *handlers) saveMapped(list []portState) {
	if len(list) == 0 {
		_ = os.Remove(h.mappedFile())
		return
	}
	if data, err := json.MarshalIndent(list, "", "  "); err == nil {
		_ = os.WriteFile(h.mappedFile(), data, 0o644)
	}
}

// upnpLoop keeps the requested forwards in place: shortly after start, every
// 20 minutes (routers reboot and forget; timed leases expire) and whenever the
// settings change. The ports come from several places saved by different
// handlers (game port in Settings, RCon port on the RCon page, steamQueryPort
// in server.cfg), so instead of trusting each of them to kick, a cheap poll
// compares the wanted forwards with what the last sync worked towards.
func (h *handlers) upnpLoop(ctx context.Context) {
	t := time.NewTimer(8 * time.Second)
	defer t.Stop()
	poll := time.NewTicker(30 * time.Second)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-h.upnpKick:
		case <-poll.C:
			if !h.upnpStale() {
				continue
			}
		}
		h.upnpSync(ctx)
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(20 * time.Minute)
	}
}

// upnpStale reports whether the settings now ask for other forwards than the
// last sync worked towards. It touches no network (only the config and
// server.cfg), so polling it is cheap.
func (h *handlers) upnpStale() bool {
	want := portsKey(h.wantedPorts(h.app.Cfg()))
	h.upnp.mu.Lock()
	defer h.upnp.mu.Unlock()
	return want != h.upnp.synced
}

func portKey(p portState) string { return fmt.Sprintf("%s/%d", p.Proto, p.Port) }

func portsKey(list []portState) string {
	keys := make([]string, len(list))
	for i, p := range list {
		keys[i] = portKey(p)
	}
	return strings.Join(keys, ",")
}

// upnpSync brings the router's port mappings in line with the settings. A
// call that arrives while a sync is running (a switch flipped mid-sync) is not
// dropped: the running sync goes round once more with the new settings.
func (h *handlers) upnpSync(ctx context.Context) {
	h.upnp.mu.Lock()
	if h.upnp.Running {
		h.upnp.again = true
		h.upnp.mu.Unlock()
		return
	}
	h.upnp.Running = true
	h.upnp.mu.Unlock()
	for {
		h.upnpSyncOnce(ctx)
		h.upnp.mu.Lock()
		h.upnp.LastRun = time.Now().Unix()
		if !h.upnp.again || ctx.Err() != nil {
			h.upnp.again = false
			h.upnp.Running = false
			h.upnp.mu.Unlock()
			return
		}
		h.upnp.again = false
		h.upnp.mu.Unlock()
	}
}

func (h *handlers) upnpSyncOnce(ctx context.Context) {
	cfg := h.app.Cfg()
	want := h.wantedPorts(cfg)
	prev := h.loadMapped()
	// Recorded up front, failed or not: a failing router is retried by the
	// 20-minute timer, not by every poll.
	h.upnp.mu.Lock()
	h.upnp.synced = portsKey(want)
	h.upnp.mu.Unlock()
	if len(want) == 0 && len(prev) == 0 {
		h.upnp.mu.Lock()
		h.upnp.Ports, h.upnp.Error = nil, ""
		h.upnp.mu.Unlock()
		return
	}

	gw, err := upnp.Discover(ctx, 3*time.Second)
	if err != nil {
		h.upnp.mu.Lock()
		h.upnp.Error = err.Error()
		h.upnp.Router, h.upnp.LocalIP, h.upnp.ExternalIP, h.upnp.Public = "", "", "", nil
		h.upnp.Ports = markAll(want, false, err.Error())
		h.upnp.mu.Unlock()
		return
	}
	ext, extErr := gw.ExternalIP(ctx)
	var public *bool
	if extErr == nil {
		p := upnp.IsPublic(ext)
		public = &p
	}

	results, still := reconcilePorts(ctx, gw, gw.LocalIP, want, prev)
	h.saveMapped(still)

	h.upnp.mu.Lock()
	h.upnp.Router, h.upnp.LocalIP, h.upnp.ExternalIP, h.upnp.Public = gw.Name, gw.LocalIP, ext, public
	h.upnp.Error = ""
	if extErr != nil {
		h.upnp.Error = extErr.Error()
	}
	h.upnp.Ports = results
	h.upnp.mu.Unlock()
	okN := 0
	for _, r := range results {
		if r.OK {
			okN++
		}
	}
	if len(results) > 0 {
		h.app.Log.Printf("UPnP: %d of %d port forward(s) active on %s (external address %s)", okN, len(results), gw.Name, ext)
	}
}

// portMapper is the part of *upnp.Gateway reconcilePorts uses (a fake in tests).
type portMapper interface {
	MappedTo(ctx context.Context, proto string, port int) (string, error)
	AddPortMapping(ctx context.Context, proto string, port int, desc string) error
	DeletePortMapping(ctx context.Context, proto string, port int) error
}

// reconcilePorts adds the wanted forwards and removes the ones we created
// earlier (prev) that are no longer wanted — but only ones still pointing at
// this machine. It returns the per-port results and the forwards this manager
// now owns (what upnp.json must remember). A forward whose state could not be
// established is kept in that list: forgetting it would leave it open on the
// router for good, with nothing ever trying to remove it again.
func reconcilePorts(ctx context.Context, gw portMapper, localIP string, want, prev []portState) (results, still []portState) {
	wanted := map[string]bool{}
	for _, p := range want {
		wanted[portKey(p)] = true
	}
	owned := map[string]bool{}
	for _, p := range prev {
		owned[portKey(p)] = true
		if wanted[portKey(p)] {
			continue
		}
		to, err := gw.MappedTo(ctx, p.Proto, p.Port)
		var se *upnp.SOAPError
		switch {
		case err == nil && to != "" && to != localIP:
			// Someone re-pointed it elsewhere: not ours any more.
		case err == nil:
			if derr := gw.DeletePortMapping(ctx, p.Proto, p.Port); derr != nil && to != "" {
				still = append(still, p)
			}
		case errors.As(err, &se) && ctx.Err() == nil:
			// The router answered but refused the lookup (some lack
			// GetSpecificPortMappingEntry): delete it blind — it is our own
			// forward — and keep remembering it only if that fails too.
			if derr := gw.DeletePortMapping(ctx, p.Proto, p.Port); derr != nil {
				still = append(still, p)
			}
		default:
			// Timeout, network error, cancelled request: unknown, retry later.
			still = append(still, p)
		}
	}
	results = make([]portState, 0, len(want))
	for _, p := range want {
		r := p
		if to, err := gw.MappedTo(ctx, p.Proto, p.Port); err == nil && to != "" && to != localIP {
			r.Error = fmt.Sprintf("the router already forwards this port to %s", to)
		} else if err := gw.AddPortMapping(ctx, p.Proto, p.Port, upnpDesc); err != nil {
			r.Error = err.Error()
			if owned[portKey(p)] {
				// Created by an earlier sync and may well still be there.
				still = append(still, p)
			}
		} else {
			r.OK = true
			still = append(still, p)
		}
		results = append(results, r)
	}
	return results, still
}

func markAll(list []portState, ok bool, msg string) []portState {
	out := make([]portState, len(list))
	for i, p := range list {
		p.OK, p.Error = ok, msg
		out[i] = p
	}
	return out
}

// ---------------------------------------------------------------- firewall

const (
	fwPanelRule = "DayZ Server Manager (panel)"
	fwGameRule  = "DayZ Server (game ports)"
)

// firewallRuleExists asks netsh (no admin needed) whether a named rule exists.
func firewallRuleExists(name string) *bool {
	if runtime.GOOS != "windows" {
		return nil
	}
	err := util.HiddenCommand("netsh", "advfirewall", "firewall", "show", "rule", "name="+name).Run()
	ok := err == nil
	return &ok
}

func (h *handlers) remoteFirewall(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "windows" {
		http.Error(w, "only available on Windows — on Linux open the ports in your firewall (e.g. ufw)", http.StatusNotImplemented)
		return
	}
	if !isLoopback(r) {
		// The UAC prompt appears on the server's screen; from a phone nobody
		// could answer it.
		authError(w, http.StatusForbidden, "remote.fw.localOnly", nil)
		return
	}
	cfg := h.app.Cfg()
	var game []string
	for _, p := range h.gamePorts(cfg) {
		game = append(game, fmt.Sprint(p.Port))
	}
	script := strings.Join([]string{
		"@echo off",
		fmt.Sprintf(`netsh advfirewall firewall delete rule name="%s" >nul 2>&1`, fwPanelRule),
		fmt.Sprintf(`netsh advfirewall firewall add rule name="%s" dir=in action=allow protocol=TCP localport=%d profile=any`, fwPanelRule, h.panelPort),
		fmt.Sprintf(`netsh advfirewall firewall delete rule name="%s" >nul 2>&1`, fwGameRule),
		fmt.Sprintf(`netsh advfirewall firewall add rule name="%s" dir=in action=allow protocol=UDP localport=%s profile=any`, fwGameRule, strings.Join(game, ",")),
	}, "\r\n") + "\r\n"
	path := filepath.Join(h.app.ManagerDir, "firewall-rules.cmd")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Elevate through PowerShell: Windows shows the UAC prompt on this PC.
	psPath := strings.ReplaceAll(path, "'", "''")
	ps := fmt.Sprintf(`Start-Process -FilePath 'cmd.exe' -ArgumentList '/c "%s"' -Verb RunAs -WindowStyle Hidden -Wait`, psPath)
	cmd := util.HiddenCommand("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	if err := cmd.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	go func() {
		_ = cmd.Wait()
		_ = os.Remove(path)
		h.app.Log.Printf("Windows Firewall rules requested (panel TCP %d, game UDP %s)", h.panelPort, strings.Join(game, ","))
	}()
	writeJSON(w, map[string]interface{}{"started": true})
}

// ---------------------------------------------------------------- status

func (h *handlers) remoteStatus(w http.ResponseWriter, r *http.Request) {
	cfg := h.app.Cfg()
	ips := util.LANAddresses()
	scheme := "http"
	if h.server != nil && h.server.httpsOn {
		scheme = "https"
	}
	lan := make([]string, 0, len(ips))
	for _, ip := range ips {
		lan = append(lan, fmt.Sprintf("%s://%s:%d/", scheme, ip, h.panelPort))
	}
	writeJSON(w, map[string]interface{}{
		"os":          runtime.GOOS,
		"exposure":    cfg.Exposure,
		"panelPort":   h.panelPort,
		"https":       h.server != nil && h.server.httpsOn,
		"httpsWanted": cfg.PanelHTTPS,
		"authEnabled": h.auth.enabled(),
		"lanUrls":     lan,
		"upnpPanel":   cfg.UPnPPanel,
		"upnpGame":    cfg.UPnPGame,
		"gamePorts":   h.gamePorts(cfg),
		"upnp":        h.upnp.view(),
		"firewall": map[string]interface{}{
			"supported": runtime.GOOS == "windows",
			"panel":     firewallRuleExists(fwPanelRule),
			"game":      firewallRuleExists(fwGameRule),
		},
		"local": isLoopback(r),
	})
}

// remoteUPnP turns the forwards on/off and runs a sync right away.
func (h *handlers) remoteUPnP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Panel *bool `json:"panel"`
		Game  *bool `json:"game"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.app.MutateConfig(func(c *config.Manager) error {
		if req.Panel != nil {
			c.UPnPPanel = *req.Panel
		}
		if req.Game != nil {
			c.UPnPGame = *req.Game
		}
		return nil
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	h.upnpSync(ctx)
	h.remoteStatus(w, r)
}

// remoteCheck diagnoses internet reachability: router + WAN address via UPnP
// and, when asked, the public address seen from the internet (one HTTPS
// request to a what-is-my-IP service — only on this explicit click).
func (h *handlers) remoteCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Public bool `json:"public"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	out := map[string]interface{}{}
	gw, err := upnp.Discover(ctx, 3*time.Second)
	if err != nil {
		out["upnpError"] = err.Error()
	} else {
		out["router"] = gw.Name
		out["localIp"] = gw.LocalIP
		if ext, err := gw.ExternalIP(ctx); err == nil {
			out["externalIp"] = ext
			out["routerPublic"] = upnp.IsPublic(ext)
		} else {
			out["externalError"] = err.Error()
		}
	}
	if req.Public {
		if ip, err := publicIP(ctx); err == nil {
			out["publicIp"] = ip
			if ext, ok := out["externalIp"].(string); ok {
				// The router's WAN address differs from what the internet sees:
				// another NAT sits in between (CGNAT) — forwards cannot work.
				out["cgnat"] = ext != ip || !upnp.IsPublic(ext)
			}
		} else {
			out["publicError"] = err.Error()
		}
	} else if ext, ok := out["externalIp"].(string); ok {
		out["cgnat"] = !upnp.IsPublic(ext)
	}
	// Without the router's own WAN address there is nothing to compare the
	// public address with: CGNAT can be neither ruled out nor confirmed.
	if _, ok := out["cgnat"]; !ok {
		out["cgnatUnknown"] = true
	}
	writeJSON(w, out)
}

func publicIP(ctx context.Context) (string, error) {
	var lastErr error
	for _, u := range []string{"https://api.ipify.org", "https://ifconfig.me/ip", "https://icanhazip.com"} {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		req, _ := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
		req.Header.Set("User-Agent", "DayZ-Server-Manager")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()
		cancel()
		ip := strings.TrimSpace(string(body))
		if resp.StatusCode == http.StatusOK && net.ParseIP(ip) != nil {
			return ip, nil
		}
		lastErr = fmt.Errorf("%s: %s", u, resp.Status)
	}
	return "", lastErr
}
