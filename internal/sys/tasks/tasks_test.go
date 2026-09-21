package tasks

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloseWaitsForRunningWork(t *testing.T) {
	g := New()
	var finished atomic.Bool
	g.Go(func(ctx context.Context) {
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond) // still writing when told to stop
		finished.Store(true)
	})
	if !g.Close(time.Second) {
		t.Fatal("Close gave up although the work finished in time")
	}
	if !finished.Load() {
		t.Fatal("Close returned before the work finished")
	}
}

func TestNothingStartsOnceClosing(t *testing.T) {
	g := New()
	g.Close(time.Second)
	var ran atomic.Bool
	g.Go(func(context.Context) { ran.Store(true) })
	time.Sleep(20 * time.Millisecond)
	if ran.Load() {
		t.Fatal("work started after Close")
	}
	if g.Enter() {
		t.Fatal("Enter let work in after Close")
	}
}

func TestCloseGivesUpOnWorkThatWontStop(t *testing.T) {
	g := New()
	release := make(chan struct{})
	defer close(release)
	g.Go(func(context.Context) { <-release })
	start := time.Now()
	if g.Close(30 * time.Millisecond) {
		t.Fatal("Close reported success with work still running")
	}
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("Close waited %v, well past its deadline", waited)
	}
}

func TestTheContextEndsOnClose(t *testing.T) {
	g := New()
	if g.Context().Err() != nil {
		t.Fatal("the context ended before Close")
	}
	g.Close(time.Second)
	if g.Context().Err() == nil {
		t.Fatal("the context outlived Close")
	}
}

func TestWorkTheCallerRunsItselfIsWaitedFor(t *testing.T) {
	g := New()
	if !g.Enter() {
		t.Fatal("Enter refused while open")
	}
	var finished atomic.Bool
	go func() {
		defer g.Leave()
		time.Sleep(50 * time.Millisecond)
		finished.Store(true)
	}()
	g.Close(time.Second)
	if !finished.Load() {
		t.Fatal("Close didn't wait for work counted with Enter")
	}
}
