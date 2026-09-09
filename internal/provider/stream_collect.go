package provider

import (
	"strings"
	"sync"
	"time"
)

// collector accumulates streaming events and turns them into a StreamResult.
// Thread-safe: the reader goroutine records events while the feeder runs.
type collector struct {
	mu           sync.Mutex
	start        time.Time
	feedEnd      time.Time
	firstPartial time.Time
	lastFinal    time.Time
	interims     int
	finals       []string
}

func (c *collector) begin(start time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.start = start
}

func (c *collector) endFeed(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.feedEnd = t
}

func (c *collector) interim(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.interims++
	if c.firstPartial.IsZero() {
		c.firstPartial = time.Now()
	}
}

func (c *collector) final(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if strings.TrimSpace(text) != "" {
		c.finals = append(c.finals, strings.TrimSpace(text))
		if c.firstPartial.IsZero() {
			c.firstPartial = now
		}
	}
	c.lastFinal = now
}

func (c *collector) result() StreamResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := StreamResult{Text: strings.Join(c.finals, " "), Interims: c.interims}
	if !c.firstPartial.IsZero() && !c.start.IsZero() {
		r.TTFPartialMS = ceilMS(c.firstPartial.Sub(c.start))
	}
	if !c.lastFinal.IsZero() && !c.feedEnd.IsZero() && c.lastFinal.After(c.feedEnd) {
		r.FinalLagMS = ceilMS(c.lastFinal.Sub(c.feedEnd))
	}
	return r
}

// ceilMS reports a positive duration as at least 1ms: a sub-millisecond
// first partial is "about 1ms", never 0 — a 0 would read as "no partial".
func ceilMS(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	ms := int(d.Milliseconds())
	if ms == 0 {
		return 1
	}
	return ms
}
