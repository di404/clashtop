package usage

import (
	"context"
	"log"
	"time"

	"github.com/di404/clashtop/internal/api"
)

const flushEvery = 15 * time.Second

// Run polls /connections and records usage until ctx ends. connect is called
// again after every failure, so a socket that appears later (Clash started
// after login) is picked up.
func Run(ctx context.Context, dir string, connect func() *api.Client, interval time.Duration) error {
	rec, err := NewRecorder(dir, time.Now())
	if err != nil {
		return err
	}
	defer func() {
		if err := rec.Flush(); err != nil {
			log.Println("flush:", err)
		}
	}()
	poll := time.NewTicker(interval)
	defer poll.Stop()
	lastFlush := time.Now()
	var client *api.Client
	failing := false
	for {
		if client == nil {
			client = connect()
		}
		snap, err := client.Connections(ctx)
		now := time.Now()
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			if !failing {
				log.Printf("%s: %v", client.Label, err)
				failing = true
			}
			client = nil
		default:
			if failing {
				log.Printf("%s: connected", client.Label)
				failing = false
			}
			if err := rec.Apply(snap, now); err != nil {
				log.Println("record:", err)
			}
		}
		if now.Sub(lastFlush) >= flushEvery {
			if err := rec.Flush(); err != nil {
				log.Println("flush:", err)
			}
			lastFlush = now
		}
		wait := poll.C
		var retry <-chan time.Time
		if client == nil {
			retry = time.After(5 * time.Second)
			wait = nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wait:
		case <-retry:
		}
	}
}
