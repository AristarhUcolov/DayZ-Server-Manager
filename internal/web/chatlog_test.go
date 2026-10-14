// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dayzmanager/internal/rcon"
)

func TestChatArchiveWriteReadPrune(t *testing.T) {
	dir := t.TempDir()
	keep := 30
	a := newChatArchive(dir, func() int { return keep }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.run(ctx); close(done) }()

	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	a.push(rcon.ChatMsg{At: yesterday.UnixMilli(), Channel: "Global", Name: "Ann", Text: "old"})
	a.push(rcon.ChatMsg{At: now.UnixMilli(), Channel: "Global", Name: "Bob", Text: "Привет"})
	a.push(rcon.ChatMsg{At: now.UnixMilli() + 1, Channel: "Global", Text: "Restart soon", Source: rcon.ChatFromManager})
	cancel() // shutdown drains the queue
	<-done

	days := a.days()
	if len(days) != 2 || days[0].Date != now.Format("2006-01-02") || days[0].Count != 2 {
		t.Fatalf("days: %+v", days)
	}
	msgs, err := a.readDay(now.Format("2006-01-02"))
	if err != nil || len(msgs) != 2 || msgs[0].Text != "Привет" || msgs[1].Source != rcon.ChatFromManager {
		t.Fatalf("readDay: %+v %v", msgs, err)
	}
	if got := a.recent(2); len(got) != 2 || got[1].Text != "Restart soon" {
		t.Fatalf("recent: %+v", got)
	}
	if got := a.recent(10); len(got) != 3 || got[0].Text != "old" {
		t.Fatalf("recent across days: %+v", got)
	}
	// Path traversal through the date is refused.
	if _, err := a.readDay("../../manager"); err == nil {
		t.Fatal("bad date accepted")
	}
	// A torn last line (crash mid-write) is skipped, not fatal.
	f := filepath.Join(dir, "chat", now.Format("2006-01-02")+".jsonl")
	fh, _ := os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = fh.WriteString(`{"at":1,"text":"tor`)
	fh.Close()
	if msgs, _ := a.readDay(now.Format("2006-01-02")); len(msgs) != 2 {
		t.Fatalf("torn line broke reading: %d", len(msgs))
	}
	// Retention.
	oldFile := filepath.Join(dir, "chat", now.AddDate(0, 0, -40).Format("2006-01-02")+".jsonl")
	_ = os.WriteFile(oldFile, []byte("{}\n"), 0o644)
	a.prune()
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("file past retention not pruned")
	}
	keep = 0 // forever
	_ = os.WriteFile(oldFile, []byte("{}\n"), 0o644)
	a.prune()
	if _, err := os.Stat(oldFile); err != nil {
		t.Fatal("retention 0 must keep everything")
	}
}
