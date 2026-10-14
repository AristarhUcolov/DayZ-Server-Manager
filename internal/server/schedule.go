// Copyright (c) 2026 Aristarh Ucolov.
//
// Restart scheduling. Every automatic restart — interval, daily HH:MM, memory,
// mod update, scheduled profile — goes through restartAt(target): a countdown
// against one absolute deadline. Announcements fire at target-N minutes, the
// optional kick/lock window opens at target-K seconds, and the restart happens
// at target. The dashboard reads the same target (NextRestart), so the panel
// and the players always see the same time.
//
// Before v1.0.0 the countdown was a chain of relative sleeps started from a
// long, pre-computed wait. A manual restart (or crash-watchdog restart) during
// that wait left the stale timer armed, so the server restarted on the OLD
// schedule while the dashboard — computing from the new uptime — still showed
// minutes to go: players were told "3 minutes" while the panel said 8.
package server

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"dayzmanager/internal/util"
)

// RestartPlan describes the next automatic restart.
type RestartPlan struct {
	At           time.Time `json:"at"`
	Reason       string    `json:"reason"`       // interval | scheduled | memory | modupdate | profile
	Active       bool      `json:"active"`       // countdown in progress (players are being warned)
	WaitingEmpty bool      `json:"waitingEmpty"` // deferred until the server is empty
	Kicking      bool      `json:"kicking"`      // inside the kick/lock window
}

// processStats is swappable for tests.
var processStats = func(pid int) (float64, uint64, error) { return util.ProcessStats(uint32(pid)) }

// tickEvery is the supervisor resolution. Five seconds keeps restarts on time
// without busy-looping.
const tickEvery = 5 * time.Second

// tick is the supervisor resolution for this controller (tests shorten it
// before starting the loops).
func (c *Controller) tick() time.Duration {
	if c.tickOverride > 0 {
		return c.tickOverride
	}
	return tickEvery
}

// defaultWarnMessage is the countdown text when none is configured.
const defaultWarnMessage = "Server restart in {m} min"

// defaultKickMessage is the kick reason when none is configured.
const defaultKickMessage = "Server restart - please reconnect in a few minutes"

// leadTime is how long before the restart moment the countdown must start: the
// earliest warning or the kick window, whichever is longer.
func leadTime(s scheduleConfig) time.Duration {
	lead := time.Duration(maxWarnMinutes(s.restartWarnMinutes)) * time.Minute
	if k := time.Duration(s.restartKickSeconds) * time.Second; k > lead {
		lead = k
	}
	return lead
}

// formatWarn renders the countdown message for m minutes left.
func formatWarn(tpl string, m int) string {
	if strings.TrimSpace(tpl) == "" {
		tpl = defaultWarnMessage
	}
	if strings.Contains(tpl, "{m}") {
		return strings.ReplaceAll(tpl, "{m}", fmt.Sprint(m))
	}
	return fmt.Sprintf("%s (%d)", tpl, m)
}

// NextRestart returns the next automatic restart the supervisor will perform,
// or nil when none is scheduled (or the server is down). An active countdown
// always wins, so the dashboard shows exactly what players were told.
func (c *Controller) NextRestart() *RestartPlan {
	if p := c.plan.Load(); p != nil {
		cp := *p
		return &cp
	}
	if !c.IsRunning() || c.loopPaused.Load() {
		return nil
	}
	s := c.schedSnapshot()
	now := time.Now()
	var best *RestartPlan
	consider := func(at time.Time, reason string) {
		if best == nil || at.Before(best.At) {
			best = &RestartPlan{At: at, Reason: reason}
		}
	}
	if s.autoRestartEnabled && s.autoRestartSeconds > 0 {
		if st := c.StartedAt(); !st.IsZero() {
			at := st.Add(time.Duration(s.autoRestartSeconds) * time.Second)
			if min := now.Add(leadTime(s)); at.Before(min) {
				at = min // overdue: the loop will count down from now
			}
			consider(at, "interval")
		}
	}
	for _, hhmm := range s.scheduledRestarts {
		if at, ok := nextOccurrence(hhmm, now); ok {
			consider(at, "scheduled")
		}
	}
	for _, sp := range s.scheduledProfiles {
		if !sp.Enabled || strings.TrimSpace(sp.Profile) == "" {
			continue
		}
		if at, ok := nextOccurrence(sp.Time, now); ok {
			consider(at, "profile")
		}
	}
	return best
}

// nextOccurrence returns the first moment at or after `after` whose wall clock
// reads hh:mm (local time), today or tomorrow.
func nextOccurrence(hhmm string, after time.Time) (time.Time, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(hhmm))
	if err != nil {
		return time.Time{}, false
	}
	at := time.Date(after.Year(), after.Month(), after.Day(), t.Hour(), t.Minute(), 0, 0, after.Location())
	if at.Before(after) {
		at = at.AddDate(0, 0, 1)
	}
	return at, true
}

// sameRun reports whether the server is still the same run the countdown was
// armed for — not stopped, not restarted by hand in the meantime.
func (c *Controller) sameRun(gen int64) bool {
	return c.IsRunning() && !c.IsStopping() && c.runGen.Load() == gen
}

// sleepUntil waits until t, waking every second to notice a stop, a manual
// restart, or manager shutdown. Returns false when the countdown must abort.
func (c *Controller) sleepUntil(t time.Time, gen int64, stop <-chan struct{}) bool {
	for {
		if !c.sameRun(gen) {
			return false
		}
		d := time.Until(t)
		if d <= 0 {
			return true
		}
		if d > time.Second {
			d = time.Second
		}
		select {
		case <-stop:
			return false
		case <-time.After(d):
		}
	}
}

func (c *Controller) loopStop() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.restartStop == nil {
		return make(chan struct{}) // never closes: loops not started (tests)
	}
	return c.restartStop
}

// say broadcasts to everyone, logging the outcome.
func (c *Controller) say(msg string) {
	if c.Broadcast == nil {
		return
	}
	if err := c.Broadcast.Say(msg); err != nil {
		c.log.Printf("rcon broadcast %q: %v", msg, err)
		return
	}
	c.log.Printf("rcon broadcast: %s", msg)
}

// restartAt runs one countdown to target and restarts the server there. Only
// one countdown runs at a time; a second trigger while one is pending is
// ignored (the pending one already covers it). The countdown aborts — without
// restarting — if the server is stopped or restarted by hand meanwhile.
func (c *Controller) restartAt(target time.Time, reason string) error {
	if !c.restarting.CompareAndSwap(false, true) {
		return nil
	}
	defer c.restarting.Store(false)
	defer c.plan.Store(nil)

	gen := c.runGen.Load()
	stop := c.loopStop()
	s := c.schedSnapshot()

	// Smart "restart only when empty": defer while players are online. If the
	// server empties, restart immediately (nobody to warn). If the max wait is
	// hit with players still on, count down from now so the restart happens.
	if s.restartOnlyWhenEmpty && c.PlayerCount != nil {
		c.plan.Store(&RestartPlan{At: target, Reason: reason, WaitingEmpty: true})
		empty := c.waitForEmpty(s.restartEmptyMaxWait, gen, stop)
		if !c.sameRun(gen) {
			return nil
		}
		if empty {
			c.log.Printf("smart restart (%s): server empty — restarting now", reason)
			c.restartReason.Store(reason)
			return c.Restart()
		}
		c.log.Printf("smart restart (%s): max wait reached with players online — counting down", reason)
		if min := time.Now().Add(leadTime(s)); target.Before(min) {
			target = min
		}
	}

	plan := &RestartPlan{At: target, Reason: reason, Active: true}
	c.plan.Store(plan)
	c.log.Printf("restart (%s) scheduled for %s — countdown started", reason, target.Format("15:04:05"))
	if mins := int(time.Until(target).Round(time.Minute).Minutes()); mins > 0 {
		c.notify("restartplanned", reason+"\x1f"+strconv.Itoa(mins))
	}

	warn := append([]int(nil), s.restartWarnMinutes...)
	sort.Sort(sort.Reverse(sort.IntSlice(warn)))
	for _, m := range warn {
		if m <= 0 {
			continue
		}
		at := target.Add(-time.Duration(m) * time.Minute)
		// A warning that is already well past (the countdown started late, e.g.
		// the server came up two minutes before a scheduled time) is skipped
		// rather than announced with a wrong number.
		if time.Until(at) < -45*time.Second {
			continue
		}
		if !c.sleepUntil(at, gen, stop) {
			c.log.Printf("restart (%s) countdown cancelled — the server was stopped or restarted", reason)
			return nil
		}
		c.say(formatWarn(s.restartWarnMessage, m))
	}

	if s.restartKickSeconds > 0 {
		at := target.Add(-time.Duration(s.restartKickSeconds) * time.Second)
		if !c.sleepUntil(at, gen, stop) {
			c.log.Printf("restart (%s) countdown cancelled — the server was stopped or restarted", reason)
			return nil
		}
		c.plan.Store(&RestartPlan{At: target, Reason: reason, Active: true, Kicking: true})
		if !c.lockAndKick(target, gen, stop, s) {
			return nil
		}
	}

	if !c.sleepUntil(target, gen, stop) {
		c.log.Printf("restart (%s) countdown cancelled — the server was stopped or restarted", reason)
		return nil
	}
	c.log.Printf("restart (%s): cycling server", reason)
	c.restartReason.Store(reason)
	return c.Restart()
}

// lockAndKick opens the pre-restart window: lock the server so nobody new can
// join, then kick everyone — repeating every few seconds so anyone who slips in
// (or reconnects) is kicked again until the restart. Returns false if the
// countdown was cancelled meanwhile (the lock is then lifted again).
func (c *Controller) lockAndKick(target time.Time, gen int64, stop <-chan struct{}, s scheduleConfig) bool {
	msg := s.restartKickMessage
	if strings.TrimSpace(msg) == "" {
		msg = defaultKickMessage
	}
	locked := false
	if s.restartLock && c.LockServer != nil {
		if err := c.LockServer(true); err != nil {
			c.log.Printf("pre-restart lock: %v (continuing with kicks)", err)
		} else {
			locked = true
			c.log.Printf("server locked for the restart")
		}
	}
	for {
		if c.KickAll != nil {
			if n, err := c.KickAll(msg); err != nil {
				c.log.Printf("pre-restart kick: %v", err)
			} else if n > 0 {
				c.log.Printf("pre-restart kick: %d player(s) disconnected", n)
			}
		}
		next := time.Now().Add(10 * time.Second)
		if next.After(target) {
			next = target
		}
		if !c.sleepUntil(next, gen, stop) {
			if locked && c.IsRunning() && c.LockServer != nil {
				_ = c.LockServer(false)
			}
			c.log.Printf("restart countdown cancelled during the kick window")
			return false
		}
		if !time.Now().Before(target) {
			return true
		}
	}
}

// waitForEmpty blocks until the server has no players online, returning true.
// Returns false when maxWaitMin (>0) elapses with players still connected, or
// when the count is unknown (RCon down) — then we can't confirm empty, so the
// caller counts down instead of deferring forever.
func (c *Controller) waitForEmpty(maxWaitMin int, gen int64, stop <-chan struct{}) bool {
	if c.PlayerCount == nil {
		return true
	}
	start := time.Now()
	for {
		if !c.sameRun(gen) || c.loopPaused.Load() {
			return true // already down / paused — nothing to wait for
		}
		switch n := c.PlayerCount(); {
		case n == 0:
			return true
		case n < 0:
			return false
		}
		if maxWaitMin > 0 && time.Since(start) >= time.Duration(maxWaitMin)*time.Minute {
			return false
		}
		select {
		case <-stop:
			return false
		case <-time.After(30 * time.Second):
		}
	}
}

// restartSoon starts a countdown that ends one lead-time from now (memory
// threshold, mod update): players still get every configured warning.
func (c *Controller) restartSoon(reason string) {
	if c.restarting.Load() {
		return
	}
	target := time.Now().Add(leadTime(c.schedSnapshot()))
	if err := c.restartAt(target, reason); err != nil {
		c.log.Printf("%s restart failed: %v", reason, err)
	}
}

// StartAutoRestartLoop starts the supervisor goroutines (once, at boot). Each
// loop gates on IsRunning() and its own config flags, so they idle harmlessly
// until the server is up and something is configured.
func (c *Controller) StartAutoRestartLoop() {
	c.mu.Lock()
	if c.restartStop != nil {
		c.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	c.restartStop = stop
	c.mu.Unlock()

	// SafeLoop: a bug in one loop must not take the manager (and with it
	// every other schedule and the crash watchdog) down.
	go util.SafeLoop(c.log, "interval restart loop", func() { c.intervalRestartLoop(stop) })
	go util.SafeLoop(c.log, "scheduled restart loop", func() { c.scheduledRestartLoop(stop) })
	go util.SafeLoop(c.log, "announcement loop", func() { c.scheduledAnnounceLoop(stop) })
	go util.SafeLoop(c.log, "interval announcement loop", func() { c.intervalAnnounceLoop(stop) })
	go util.SafeLoop(c.log, "mod update check loop", func() { c.modUpdateCheckLoop(stop) })
	go util.SafeLoop(c.log, "memory restart loop", func() { c.memRestartLoop(stop) })
}

func (c *Controller) StopAutoRestartLoop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.restartStop != nil {
		close(c.restartStop)
		c.restartStop = nil
	}
}

// intervalRestartLoop cycles the server every AutoRestartSeconds of uptime
// (the classic .bat `timeout 14390 && taskkill && goto start`). The deadline is
// re-derived from the CURRENT process's start time on every tick, so a manual
// or watchdog restart moves it — no stale timer can fire on an old schedule.
func (c *Controller) intervalRestartLoop(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-time.After(c.tick()):
		}
		s := c.schedSnapshot()
		if !s.autoRestartEnabled || s.autoRestartSeconds <= 0 || !c.IsRunning() || c.loopPaused.Load() || c.restarting.Load() {
			continue
		}
		started := c.StartedAt()
		if started.IsZero() {
			continue
		}
		target := started.Add(time.Duration(s.autoRestartSeconds) * time.Second)
		if time.Until(target) > leadTime(s) {
			continue
		}
		// Overdue (the interval was just enabled on a long-running or adopted
		// server): give players the full countdown instead of restarting now.
		if min := time.Now().Add(leadTime(s)); target.Before(min) {
			target = min
		}
		if err := c.restartAt(target, "interval"); err != nil {
			c.log.Printf("interval auto-restart failed: %v", err)
		}
	}
}

// scheduledRestartLoop restarts the server exactly at each configured daily
// HH:MM (server local time), starting the countdown one lead-time early so
// every warning lands on time. Scheduled config profiles use the same path:
// the profile is staged and applied inside that restart's down-window.
func (c *Controller) scheduledRestartLoop(stop <-chan struct{}) {
	fired := map[string]bool{} // target instant (RFC3339) → handled
	for {
		select {
		case <-stop:
			return
		case <-time.After(c.tick()):
		}
		if !c.IsRunning() || c.loopPaused.Load() || c.restarting.Load() {
			continue
		}
		s := c.schedSnapshot()
		lead := leadTime(s)
		now := time.Now()
		// Look 30s back so a tick that lands just after hh:mm still fires.
		from := now.Add(-30 * time.Second)

		type trigger struct {
			at      time.Time
			reason  string
			profile string
		}
		var due *trigger
		for _, hhmm := range s.scheduledRestarts {
			if at, ok := nextOccurrence(hhmm, from); ok && at.Sub(now) <= lead && !fired[at.Format(time.RFC3339)] {
				if due == nil || at.Before(due.at) {
					due = &trigger{at: at, reason: "scheduled"}
				}
			}
		}
		if c.ApplyProfile != nil {
			for _, sp := range s.scheduledProfiles {
				slug := strings.TrimSpace(sp.Profile)
				if !sp.Enabled || slug == "" {
					continue
				}
				if at, ok := nextOccurrence(sp.Time, from); ok && at.Sub(now) <= lead && !fired[at.Format(time.RFC3339)+"|"+slug] {
					if due == nil || at.Before(due.at) || (at.Equal(due.at) && due.profile == "") {
						due = &trigger{at: at, reason: "profile", profile: slug}
					}
				}
			}
		}
		if due == nil {
			continue
		}
		key := due.at.Format(time.RFC3339)
		fired[key] = true
		if due.profile != "" {
			fired[key+"|"+due.profile] = true
			c.log.Printf("scheduled profile %q at %s — restarting to apply", due.profile, due.at.Format("15:04"))
		}
		// Forget old entries so the map can't grow forever.
		for k := range fired {
			if t, err := time.Parse(time.RFC3339, strings.SplitN(k, "|", 2)[0]); err == nil && now.Sub(t) > 48*time.Hour {
				delete(fired, k)
			}
		}
		if err := c.restartWithProfile(due.at, due.reason, due.profile); err != nil {
			c.log.Printf("scheduled restart failed: %v", err)
		}
	}
}

// restartWithProfile stages profile (if any) for this restart's down-window and
// runs the countdown. A profile the restart did not consume — the countdown was
// cancelled by a manual stop, or another countdown already held the slot — is
// dropped: left staged, it would be applied by some unrelated restart days
// later, silently overwriting every config edit made in between.
func (c *Controller) restartWithProfile(target time.Time, reason, profile string) error {
	if profile != "" {
		c.pendingMu.Lock()
		c.pendingProf = profile
		c.pendingMu.Unlock()
	}
	err := c.restartAt(target, reason)
	if profile != "" {
		c.pendingMu.Lock()
		if c.pendingProf == profile {
			c.pendingProf = ""
			c.log.Printf("scheduled profile %q not applied — the restart did not happen", profile)
		}
		c.pendingMu.Unlock()
	}
	return err
}

// memRestartLoop cycles the server when the DayZServer process RSS crosses the
// configured threshold (memory creep on long uptimes).
func (c *Controller) memRestartLoop(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-time.After(time.Minute):
		}
		s := c.schedSnapshot()
		if !s.restartOnHighMem || s.restartMemThreshMB <= 0 || !c.IsRunning() || c.loopPaused.Load() || c.restarting.Load() {
			continue
		}
		pid := c.PID()
		if pid <= 0 {
			continue
		}
		_, memBytes, err := processStats(pid)
		if err != nil || memBytes == 0 {
			continue
		}
		if memBytes >= uint64(s.restartMemThreshMB)*1024*1024 {
			c.log.Printf("smart restart: memory %d MB >= threshold %d MB — restarting",
				memBytes/1024/1024, s.restartMemThreshMB)
			c.restartSoon("memory")
		}
	}
}

// modUpdateCheckLoop polls the mod source on the configured cadence and, when
// an active mod is newer there than on the server, triggers an update+restart.
// The mods are actually copied during the restart down-window via
// beforeRestart (forced once here), so file locks are clear.
func (c *Controller) modUpdateCheckLoop(stop <-chan struct{}) {
	var lastCheck time.Time
	for {
		select {
		case <-stop:
			return
		case <-time.After(time.Minute):
		}
		s := c.schedSnapshot()
		if s.autoUpdateCheckMinutes <= 0 || s.modSourceDir == "" || c.ModUpdatesAvailable == nil {
			continue
		}
		if !c.IsRunning() || c.loopPaused.Load() || c.restarting.Load() {
			continue
		}
		if time.Since(lastCheck) < time.Duration(s.autoUpdateCheckMinutes)*time.Minute {
			continue
		}
		lastCheck = time.Now()
		avail, err := c.ModUpdatesAvailable(s.modSourceDir)
		if err != nil {
			c.log.Printf("mod update check: %v", err)
			continue
		}
		if !avail {
			continue
		}
		c.log.Printf("mod update available — updating and restarting")
		c.forceModUpdate.Store(true)
		go util.SafeGo(c.log, "mod update restart", func() { c.restartSoon("modupdate") })
	}
}

// scheduledAnnounceLoop fires once per minute and broadcasts any enabled
// announcement whose Time matches current HH:MM. A dedup map prevents
// firing the same announcement twice within its minute window in case the
// loop gets delayed and wakes up twice inside the same minute.
func (c *Controller) scheduledAnnounceLoop(stop <-chan struct{}) {
	fired := map[string]string{} // key → last yyyy-mm-ddTHH:MM we fired
	for {
		now := time.Now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-stop:
			return
		case <-time.After(time.Until(next)):
		}
		if c.Broadcast == nil || !c.IsRunning() {
			continue
		}
		hhmm := time.Now().Format("15:04")
		stamp := time.Now().Format("2006-01-02T15:04")
		for i, a := range c.schedSnapshot().announcements {
			if !a.Enabled || strings.TrimSpace(a.Time) != hhmm {
				continue
			}
			key := fmt.Sprintf("%d:%s", i, a.Time)
			if fired[key] == stamp {
				continue
			}
			fired[key] = stamp
			msg := a.Message
			go util.SafeGo(c.log, "restart warning", func() { c.say(msg) })
		}
	}
}

// intervalAnnounceLoop broadcasts each enabled interval announcement every
// IntervalMinutes while the server is running. The per-item timer resets while
// the server is down, so "every 30 min" counts uptime, not wall-clock through a
// restart/outage.
func (c *Controller) intervalAnnounceLoop(stop <-chan struct{}) {
	lastFired := map[int]time.Time{} // snapshot index → last broadcast time
	for {
		now := time.Now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-stop:
			return
		case <-time.After(time.Until(next)):
		}
		if c.Broadcast == nil || !c.IsRunning() {
			lastFired = map[int]time.Time{} // reset timers while down
			continue
		}
		now = time.Now()
		for i, a := range c.schedSnapshot().intervalAnnounces {
			if !a.Enabled || a.IntervalMinutes <= 0 || strings.TrimSpace(a.Message) == "" {
				continue
			}
			lf, seen := lastFired[i]
			if !seen {
				lastFired[i] = now // baseline: first fire one interval from now
				continue
			}
			if now.Sub(lf) >= time.Duration(a.IntervalMinutes)*time.Minute {
				lastFired[i] = now
				msg := a.Message
				go util.SafeGo(c.log, "interval announcement", func() { c.say(msg) })
			}
		}
	}
}

// maxWarnMinutes returns the largest entry in the warning list (0 if empty).
func maxWarnMinutes(warn []int) int {
	m := 0
	for _, w := range warn {
		if w > m {
			m = w
		}
	}
	return m
}

// shiftHHMM parses an "HH:MM" string, shifts it by deltaMinutes (may be
// negative), and re-formats as "HH:MM", wrapping across midnight. Returns the
// input unchanged if it isn't a valid time.
func shiftHHMM(hhmm string, deltaMinutes int) string {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return hhmm
	}
	return t.Add(time.Duration(deltaMinutes) * time.Minute).Format("15:04")
}
