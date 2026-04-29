// Copyright (c) 2026 Aristarh Ucolov.
//
// Steam Workshop "is this item updated?" probe. Hits the public
// ISteamRemoteStorage/GetPublishedFileDetails endpoint — no API key needed
// for public files. Used by the Mods page to flag mods whose Workshop copy
// is out of date relative to Steam (i.e. the user hasn't run Steam to pull
// the new files yet).

package mods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WorkshopRemote is the slice of GetPublishedFileDetails we care about.
type WorkshopRemote struct {
	PublishedID string    `json:"publishedId"`
	Title       string    `json:"title"`
	TimeUpdated time.Time `json:"timeUpdated"`
	TimeCreated time.Time `json:"timeCreated"`
	Result      int       `json:"result"` // 1 = OK; non-1 means the ID was not found / removed
}

// FetchWorkshopMeta resolves Steam Workshop metadata for the given published
// file IDs. Empty / non-numeric IDs are skipped; the returned map is keyed by
// the input ID exactly as passed (so the caller can match it back).
//
// Steam's endpoint accepts up to a few hundred IDs per call; we batch in 100s
// just to keep request bodies modest and to surface partial progress on
// failure.
func FetchWorkshopMeta(ctx context.Context, ids []string) (map[string]WorkshopRemote, error) {
	out := map[string]WorkshopRemote{}
	cleaned := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		// Trivial sanity: published file IDs are decimal numbers.
		ok := true
		for _, r := range id {
			if r < '0' || r > '9' {
				ok = false
				break
			}
		}
		if ok {
			cleaned = append(cleaned, id)
		}
	}
	if len(cleaned) == 0 {
		return out, nil
	}

	const batchSize = 100
	for i := 0; i < len(cleaned); i += batchSize {
		end := i + batchSize
		if end > len(cleaned) {
			end = len(cleaned)
		}
		if err := fetchBatch(ctx, cleaned[i:end], out); err != nil {
			return out, err
		}
	}
	return out, nil
}

func fetchBatch(ctx context.Context, ids []string, out map[string]WorkshopRemote) error {
	form := url.Values{}
	form.Set("itemcount", fmt.Sprintf("%d", len(ids)))
	for i, id := range ids {
		form.Set(fmt.Sprintf("publishedfileids[%d]", i), id)
	}
	endpoint := "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/"

	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(rctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "DayZ-Server-Manager (workshop-check)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("steam: HTTP %d", resp.StatusCode)
	}
	var raw struct {
		Response struct {
			ResultCount       int `json:"resultcount"`
			PublishedFileDtls []struct {
				PublishedFileID string `json:"publishedfileid"`
				Result          int    `json:"result"`
				Title           string `json:"title"`
				TimeCreated     int64  `json:"time_created"`
				TimeUpdated     int64  `json:"time_updated"`
			} `json:"publishedfiledetails"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("steam: decode: %w", err)
	}
	if raw.Response.PublishedFileDtls == nil {
		return errors.New("steam: empty response")
	}
	for _, d := range raw.Response.PublishedFileDtls {
		out[d.PublishedFileID] = WorkshopRemote{
			PublishedID: d.PublishedFileID,
			Title:       d.Title,
			TimeUpdated: timeFromUnix(d.TimeUpdated),
			TimeCreated: timeFromUnix(d.TimeCreated),
			Result:      d.Result,
		}
	}
	return nil
}

func timeFromUnix(s int64) time.Time {
	if s <= 0 {
		return time.Time{}
	}
	return time.Unix(s, 0).UTC()
}
