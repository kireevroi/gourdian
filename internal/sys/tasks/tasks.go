// Package tasks runs background work that has to finish before the program exits, and stops new
// work from starting once it is closing.
package tasks

import (
	"context"
	"sync"
	"time"
)

type Group struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	closing bool
	wg      sync.WaitGroup
}

func New() *Group {
	ctx, cancel := context.WithCancel(context.Background())
	return &Group{ctx: ctx, cancel: cancel}
}

// Context ends when the group closes.
func (g *Group) Context() context.Context { return g.ctx }

// Go runs work with the group's context, and does nothing once the group is closing.
func (g *Group) Go(work func(ctx context.Context)) {
	if !g.Enter() {
		return
	}
	go func() {
		defer g.Leave()
		work(g.ctx)
	}()
}

// Enter counts work the caller runs itself, and reports false once the group is closing.
func (g *Group) Enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return false
	}
	g.wg.Add(1)
	return true
}

func (g *Group) Leave() { g.wg.Done() }

// Close stops new work, ends the context and waits up to wait, reporting false if work outlived it.
func (g *Group) Close(wait time.Duration) bool {
	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()
	g.cancel()
	done := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(wait):
		return false
	}
}
