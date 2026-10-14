// Copyright (c) 2026 Aristarh Ucolov.
package app

// Multi-server: one manager runs several DayZ servers. The server the manager
// started in is the primary ("main"); more are added from the Servers page.
// Every server is a full App over its own folder — its own .dayz-manager
// (settings, logs, players, chat, backups), process supervisor, RCon and
// scheduler — exactly what a separate manager copy in that folder would have.
// Only panel-wide settings (language, exposure, HTTPS, router forwards and the
// login) live with the primary.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dayzmanager/internal/config"
	"dayzmanager/internal/server"
	"dayzmanager/internal/util"
)

// PrimaryID names the server the manager runs from.
const PrimaryID = "main"

// MaxServers caps how many servers one manager runs (each is a full DayZ
// server process — far more than one PC hosts).
const MaxServers = 16

// Member is one server of the fleet. App is nil when the server could not be
// opened (its folder is gone, a drive is unplugged); Error then says why, and
// the entry stays so it can be fixed or removed.
type Member struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	App   *App   `json:"-"`
	Error string `json:"error,omitempty"`
}

// Fleet is every DayZ server this manager runs.
type Fleet struct {
	Primary *App

	mu      sync.Mutex
	members []*Member // the primary first, then added servers in order
	path    string    // servers.json in the primary's manager folder
}

type fleetEntry struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Dir  string `json:"dir,omitempty"` // empty for the primary
}

type fleetFile struct {
	Servers []fleetEntry `json:"servers"`
}

// OpenFleet loads the servers added to the panel (servers.json next to the
// primary's settings) and opens each of them.
func OpenFleet(primary *App) *Fleet {
	f := &Fleet{Primary: primary, path: filepath.Join(primary.ManagerDir, "servers.json")}
	f.members = []*Member{{ID: PrimaryID, Name: primary.Label(), Dir: primary.ServerDir, App: primary}}
	primary.Server.PreStart = func() error { return f.portClash(primary) }

	data, err := os.ReadFile(f.path)
	if err != nil {
		return f
	}
	var ff fleetFile
	if err := json.Unmarshal(data, &ff); err != nil {
		primary.Log.Printf("servers.json is unreadable (%v) — only this server is shown", err)
		return f
	}
	seen := map[string]bool{PrimaryID: true}
	for _, e := range ff.Servers {
		if e.ID == PrimaryID {
			if name := cleanName(e.Name); name != "" {
				f.members[0].Name = name
				primary.SetLabel(name)
			}
			continue
		}
		if e.ID == "" || seen[e.ID] || e.Dir == "" || len(f.members) >= MaxServers {
			continue
		}
		seen[e.ID] = true
		m := &Member{ID: e.ID, Name: cleanName(e.Name), Dir: filepath.Clean(e.Dir)}
		if m.Name == "" {
			m.Name = filepath.Base(m.Dir)
		}
		if err := f.ownedElsewhere(m.Dir); err != nil {
			m.Error = err.Error()
			primary.Log.Printf("server %q (%s) is not opened: %v", m.Name, m.Dir, err)
		} else if err := f.open(m); err != nil {
			m.Error = err.Error()
			primary.Log.Printf("server %q (%s) could not be opened: %v", m.Name, m.Dir, err)
		}
		f.members = append(f.members, m)
	}
	return f
}

// open creates the App for an added server. The folder must still hold a DayZ
// server: opening a missing one would create an empty .dayz-manager tree where
// a drive used to be.
func (f *Fleet) open(m *Member) error {
	st, err := os.Stat(m.Dir)
	if err != nil || !st.IsDir() {
		return errors.New("the server folder was not found")
	}
	if _, ok := server.FindServerExe(m.Dir); !ok {
		return errors.New("no DayZ server (DayZServer_x64.exe / DayZServer) in this folder")
	}
	a, err := NewServer(m.Dir, f.Primary, m.ID, m.Name)
	if err != nil {
		return err
	}
	a.Server.PreStart = func() error { return f.portClash(a) }
	m.App, m.Error = a, ""
	return nil
}

// ownedElsewhere refuses a folder another, still running manager runs. It
// asks that manager over HTTP — never call it holding f.mu: the address could
// be this very panel, whose handlers take f.mu.
func (f *Fleet) ownedElsewhere(dir string) error {
	if url, busy := OwnerOf(dir, f.Primary.ManagerDir); busy {
		return fmt.Errorf("another DayZ Server Manager runs this server (%s)", url)
	}
	return nil
}

// Members returns a copy of the server list (the primary first).
func (f *Fleet) Members() []Member {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Member, len(f.members))
	for i, m := range f.members {
		out[i] = *m
	}
	return out
}

// Apps returns every opened server.
func (f *Fleet) Apps() []*App {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*App, 0, len(f.members))
	for _, m := range f.members {
		if m.App != nil {
			out = append(out, m.App)
		}
	}
	return out
}

// Get returns the opened server with this id (nil when unknown or not open).
func (f *Fleet) Get(id string) *App {
	if id == "" || id == PrimaryID {
		return f.Primary
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.members {
		if m.ID == id {
			return m.App
		}
	}
	return nil
}

// Multi reports whether more than one server is set up.
func (f *Fleet) Multi() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.members) > 1
}

// AnyRunning reports whether any server of the fleet is running.
func (f *Fleet) AnyRunning() bool {
	for _, a := range f.Apps() {
		if a.ServerIsRunning() {
			return true
		}
	}
	return false
}

// Add puts the DayZ server in dir under the panel's management.
func (f *Fleet) Add(dir, name string) (*App, error) {
	dir, err := cleanDir(dir)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return nil, errors.New("folder not found: " + dir)
	}
	if _, ok := server.FindServerExe(dir); !ok {
		return nil, errors.New("this folder does not hold a DayZ server (DayZServer_x64.exe / DayZServer) — install the server there first, or use “Create a copy”")
	}
	name = cleanName(name)
	if name == "" {
		name = filepath.Base(dir)
	}
	if err := f.ownedElsewhere(dir); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.members) >= MaxServers {
		return nil, fmt.Errorf("one manager runs at most %d servers", MaxServers)
	}
	if err := f.overlapLocked(dir); err != nil {
		return nil, err
	}
	m := &Member{ID: newServerID(f.members), Name: name, Dir: dir}
	if err := f.open(m); err != nil {
		return nil, err
	}
	f.members = append(f.members, m)
	if err := f.saveLocked(); err != nil {
		m.App.Detach()
		f.members = f.members[:len(f.members)-1]
		return nil, err
	}
	m.App.Log.Printf("server added to the panel: %s", dir)
	return m.App, nil
}

// CheckNewDir validates a folder for a new server before anything is copied
// into it: absolute, not inside or around a server of the panel.
func (f *Fleet) CheckNewDir(dir string) (string, error) {
	dir, err := cleanDir(dir)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return dir, f.overlapLocked(dir)
}

// overlapLocked refuses a folder that is, contains, or sits inside the folder
// of a server already in the panel: their files would mix (mods, missions,
// .dayz-manager) and one server's file editor would reach into the other.
func (f *Fleet) overlapLocked(dir string) error {
	for _, m := range f.members {
		switch {
		case samePath(m.Dir, dir):
			return fmt.Errorf("this folder is already in the panel as “%s”", m.Name)
		case within(m.Dir, dir):
			return fmt.Errorf("this folder is inside the folder of “%s” — each server needs its own folder", m.Name)
		case within(dir, m.Dir):
			return fmt.Errorf("the folder of “%s” is inside this one — each server needs its own folder", m.Name)
		}
	}
	return nil
}

// Remove takes a server off the panel. Nothing on disk is deleted — the
// folder, its settings and logs stay, and it can be added again.
func (f *Fleet) Remove(id string) error {
	if id == PrimaryID {
		return errors.New("the server the manager runs from cannot be removed")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.members {
		if m.ID != id {
			continue
		}
		if m.App != nil && m.App.ServerIsRunning() {
			return errors.New("stop this server first")
		}
		f.members = append(f.members[:i], f.members[i+1:]...)
		if err := f.saveLocked(); err != nil {
			f.members = append(f.members[:i], append([]*Member{m}, f.members[i:]...)...)
			return err
		}
		if m.App != nil {
			m.App.Log.Printf("server removed from the panel (files kept)")
			m.App.Detach()
			ReleaseOwner(m.App.ManagerDir)
			m.App.CloseLog()
		}
		return nil
	}
	return errors.New("unknown server")
}

// Rename sets a server's display name.
func (f *Fleet) Rename(id, name string) error {
	name = cleanName(name)
	if name == "" {
		return errors.New("name required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.members {
		if m.ID != id {
			continue
		}
		old := m.Name
		m.Name = name
		if err := f.saveLocked(); err != nil {
			m.Name = old
			return err
		}
		if m.App != nil {
			m.App.SetLabel(name)
		}
		return nil
	}
	return errors.New("unknown server")
}

// Retry re-opens a server that could not be opened at start-up (the drive is
// back, the server was reinstalled).
func (f *Fleet) Retry(id string) (*App, error) {
	for _, m := range f.Members() {
		if m.ID == id && m.App == nil {
			if err := f.ownedElsewhere(m.Dir); err != nil {
				return nil, err
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.members {
		if m.ID != id {
			continue
		}
		if m.App != nil {
			return m.App, nil
		}
		if err := f.open(m); err != nil {
			m.Error = err.Error()
			return nil, err
		}
		return m.App, nil
	}
	return nil, errors.New("unknown server")
}

// Move shifts a server one place up (delta -1) or down (+1) in the list. The
// primary stays first.
func (f *Fleet) Move(id string, delta int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.members {
		if m.ID != id || i == 0 {
			continue
		}
		j := i + delta
		if j < 1 || j >= len(f.members) {
			return nil
		}
		f.members[i], f.members[j] = f.members[j], f.members[i]
		return f.saveLocked()
	}
	return nil
}

func (f *Fleet) saveLocked() error {
	ff := fleetFile{}
	for _, m := range f.members {
		e := fleetEntry{ID: m.ID, Name: m.Name}
		if m.ID != PrimaryID {
			e.Dir = m.Dir
		}
		ff.Servers = append(ff.Servers, e)
	}
	data, err := json.MarshalIndent(ff, "", "  ")
	if err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

// Ports is what a server listens on: the game port and the three after it,
// the Steam query port and the RCon port.
type Ports struct {
	Game  int `json:"game"`
	Query int `json:"query"`
	RCon  int `json:"rcon"`
}

// Ports reads the server's ports from its settings.
func (a *App) Ports() Ports {
	cfg := a.Cfg()
	p := Ports{Game: cfg.ServerPort, Query: a.SteamQueryPort(), RCon: a.RConPort()}
	if p.Game <= 0 {
		p.Game = 2302
	}
	return p
}

// RConPort is the port RCon connects to (settings, else beserver_x64.cfg, else
// game port + 4).
func (a *App) RConPort() int {
	cfg := a.Cfg()
	if cfg.RConPort > 0 {
		return cfg.RConPort
	}
	if be := config.FindBEConfig(a.beResolvedDir()); be != nil && be.RConPort > 0 {
		return be.RConPort
	}
	return cfg.ServerPort + 4
}

func (p Ports) list() map[int]string {
	out := map[int]string{}
	for i := 0; i < 4; i++ {
		out[p.Game+i] = "game"
	}
	if p.Query > 0 {
		out[p.Query] = "query"
	}
	if p.RCon > 0 {
		out[p.RCon] = "rcon"
	}
	return out
}

// PortClashes lists, per other server of the panel, the ports this server
// shares with it (whether they run or not) — two servers on one port cannot
// both start. One line per server: “Name”: 2302–2305, 2306, 27016.
func (f *Fleet) PortClashes(a *App) []string {
	mine := a.Ports().list()
	var out []string
	for _, o := range f.Apps() {
		if o == a {
			continue
		}
		var shared []int
		for port := range o.Ports().list() {
			if _, ok := mine[port]; ok {
				shared = append(shared, port)
			}
		}
		if len(shared) > 0 {
			out = append(out, "“"+o.Label()+"”: "+portRanges(shared))
		}
	}
	return out
}

// portRanges renders ports sorted, runs collapsed: "2302–2305, 2306, 27016".
func portRanges(ports []int) string {
	sort.Ints(ports)
	var parts []string
	for i := 0; i < len(ports); {
		j := i
		for j+1 < len(ports) && ports[j+1] == ports[j]+1 {
			j++
		}
		if j > i+1 {
			parts = append(parts, fmt.Sprintf("%d–%d", ports[i], ports[j]))
		} else {
			for k := i; k <= j; k++ {
				parts = append(parts, strconv.Itoa(ports[k]))
			}
		}
		i = j + 1
	}
	return strings.Join(parts, ", ")
}

// portClash is a server's PreStart check: refuse to start on a port another
// RUNNING server of the panel already uses — DayZ would fail to bind it, or
// worse, both would fight over the Steam query port.
func (f *Fleet) portClash(a *App) error {
	mine := a.Ports().list()
	for _, o := range f.Apps() {
		if o == a || !o.ServerIsRunning() {
			continue
		}
		for port := range o.Ports().list() {
			if _, ok := mine[port]; ok {
				return fmt.Errorf("port %d is already used by the running server “%s” — give this server other ports (Server → game port, steamQueryPort in serverDZ.cfg, RCon port)", port, o.Label())
			}
		}
	}
	return nil
}

// FreePorts suggests ports for a new server: the first game port (in steps of
// 100 from 2302) whose block and RCon port (game + 4) no server of the panel
// uses, and the first free Steam query port from 27016 up — the usual
// 27016, 27017, … of hosts running several DayZ servers.
func (f *Fleet) FreePorts() Ports { return f.freePortsExcept(nil) }

// freePortsExcept is FreePorts ignoring the ports of server skip (the one
// that is being given new ports).
func (f *Fleet) freePortsExcept(skip *App) Ports {
	used := map[int]bool{}
	for _, a := range f.Apps() {
		if a == skip {
			continue
		}
		for p := range a.Ports().list() {
			used[p] = true
		}
	}
	out := Ports{Game: 2402, RCon: 2406, Query: 27017}
	for game := 2302; game < 60000; game += 100 {
		p := Ports{Game: game, RCon: game + 4}
		ok := true
		for port := range p.list() {
			if used[port] {
				ok = false
				break
			}
		}
		if ok {
			out.Game, out.RCon = p.Game, p.RCon
			break
		}
	}
	for q := 27016; q < 27116; q++ {
		if !used[q] && (q < out.Game || q > out.Game+4) {
			out.Query = q
			break
		}
	}
	return out
}

// AssignFreePorts gives a stopped server ports no other server of the panel
// uses: the game port in its settings, the RCon port in its settings and
// BattlEye's config, steamQueryPort in its server.cfg.
func (f *Fleet) AssignFreePorts(a *App) (Ports, error) {
	if a.ServerIsRunning() {
		return Ports{}, errors.New("stop this server first")
	}
	p := f.freePortsExcept(a)
	if err := a.MutateConfig(func(c *config.Manager) error {
		c.ServerPort, c.RConPort = p.Game, p.RCon
		return nil
	}); err != nil {
		return p, err
	}
	cfg := a.Cfg()
	cfgPath := filepath.Join(a.ServerDir, cfg.ServerCfg)
	if sc, err := config.LoadServerCfg(cfgPath); err == nil {
		sc.Set("steamQueryPort", p.Query)
		if err := sc.Save(cfgPath); err != nil {
			return p, fmt.Errorf("server.cfg: %w", err)
		}
	}
	password := cfg.RConPassword
	if password == "" {
		if be := config.FindBEConfig(a.beResolvedDir()); be != nil {
			password = be.RConPassword
		}
	}
	if password != "" {
		if _, err := config.EnsureBEConfig(a.beResolvedDir(), password, p.RCon, true); err != nil {
			return p, fmt.Errorf("beserver_x64.cfg: %w", err)
		}
	}
	a.ApplyRConConfig()
	a.Log.Printf("ports changed: game %d, query %d, RCon %d", p.Game, p.Query, p.RCon)
	return p, nil
}

// ------------------------------------------------------------------ owner file

// ownerFile marks which manager runs a server folder, so a second manager —
// another panel's Servers page, or a manager copy started inside an added
// server's folder — does not supervise the same DayZ server twice.
type ownerFile struct {
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Primary string `json:"primary"` // the owning manager's own .dayz-manager folder
	At      string `json:"at"`
}

const ownerName = "owner.json"

// ClaimOwner records this manager (listening on port) as the one running the
// server whose manager folder is managerDir.
func ClaimOwner(managerDir, primaryManagerDir string, port int) {
	data, _ := json.Marshal(ownerFile{PID: os.Getpid(), Port: port, Primary: primaryManagerDir,
		At: time.Now().Format(time.RFC3339)})
	_ = os.WriteFile(filepath.Join(managerDir, ownerName), data, 0o644)
}

// ReleaseOwner removes the mark when this manager stops running the server.
func ReleaseOwner(managerDir string) {
	data, err := os.ReadFile(filepath.Join(managerDir, ownerName))
	if err != nil {
		return
	}
	var o ownerFile
	if json.Unmarshal(data, &o) == nil && o.PID == os.Getpid() {
		_ = os.Remove(filepath.Join(managerDir, ownerName))
	}
}

// OwnerOf reports whether another, still running manager runs the server in
// serverDir, and its panel address. self is the asking manager's own primary
// manager folder (its own claims never count). A mark left by a manager that
// is gone (crash, reboot — the PID may even be reused) is ignored: the owner
// must still answer on its port and list this folder.
func OwnerOf(serverDir, self string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(serverDir, ".dayz-manager", ownerName))
	if err != nil {
		return "", false
	}
	var o ownerFile
	if json.Unmarshal(data, &o) != nil || o.Port <= 0 {
		return "", false
	}
	if o.PID == os.Getpid() || samePath(o.Primary, self) {
		return "", false
	}
	if o.PID > 0 && !util.ProcessAlive(o.PID) && runtime.GOOS != "darwin" {
		return "", false
	}
	url := "http://127.0.0.1:" + strconv.Itoa(o.Port) + "/"
	if !managesDir(url, serverDir) {
		return "", false
	}
	return url, true
}

// managesDir asks the manager at url whether it runs serverDir.
func managesDir(url, serverDir string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url + "api/info")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var info struct {
		ServerDir string   `json:"serverDir"`
		Servers   []string `json:"managedDirs"`
	}
	if json.NewDecoder(resp.Body).Decode(&info) != nil {
		return false
	}
	if samePath(info.ServerDir, serverDir) {
		return true
	}
	for _, d := range info.Servers {
		if samePath(d, serverDir) {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------ helpers

func cleanDir(dir string) (string, error) {
	dir = strings.TrimSpace(strings.Trim(strings.TrimSpace(dir), `"`))
	if dir == "" {
		return "", errors.New("folder required")
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("give the full path of the folder (for example D:\\DayZServer2 or /home/dayz/server2)")
	}
	return filepath.Clean(dir), nil
}

func cleanName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	for utf8.RuneCountInString(name) > 40 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// within reports whether path is inside dir (not equal to it).
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == "." {
		return false
	}
	if runtime.GOOS == "windows" {
		// Rel is case-sensitive; compare case-insensitively on Windows.
		rel2, err2 := filepath.Rel(strings.ToLower(dir), strings.ToLower(path))
		if err2 == nil {
			rel = rel2
		}
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func newServerID(existing []*Member) string {
	for {
		var b [3]byte
		_, _ = rand.Read(b[:])
		id := "s" + hex.EncodeToString(b[:])
		clash := false
		for _, m := range existing {
			if m.ID == id {
				clash = true
			}
		}
		if !clash {
			return id
		}
	}
}
