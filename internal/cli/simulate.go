package cli

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/sim"
)

func simulateCmd(args []string) error {
	fs := flag.NewFlagSet("simulate", flag.ExitOnError)
	speed := fs.Float64("speed", 10, "game seconds per real second")
	from := fs.Int("from", -60, "starting game clock in seconds")
	to := fs.Int("to", 1590, "ending game clock in seconds")
	url := fs.String("url", "", "trainer GSI endpoint (default from config.json)")
	random := fs.Bool("random", false, "vary hero, farm, deaths and result instead of playing the fixed script")
	fs.Parse(args)
	if *speed <= 0 {
		return errors.New("-speed must be positive")
	}
	var seed uint64
	if *random {
		seed = uint64(time.Now().UnixNano())
	}

	store, _, err := config.OpenDefault()
	if err != nil {
		return err
	}
	cfg := store.Get()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := sim.Options{URL: cmp.Or(*url, config.GSIURI(cfg.Listen)), Token: cfg.Token, Speed: *speed, From: *from, To: *to, Seed: seed}
	fmt.Printf("simulating a match into %s at %gx speed (Ctrl+C to stop)\n", opts.URL, *speed)
	err = sim.Run(ctx, opts, func(clock, status int) {
		if clock%60 == 0 {
			fmt.Printf("  clock %d:00\n", clock/60)
		}
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if err == nil {
		fmt.Println("match finished; check the dashboard's recent matches")
	}
	return err
}

func replayCmd(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	speed := fs.Float64("speed", 1, "playback speed multiplier")
	url := fs.String("url", "", "trainer GSI endpoint (default from config.json)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() != 1 || *speed <= 0 {
		return errors.New("usage: gourdian replay [-speed N] FILE")
	}
	store, _, err := config.OpenDefault()
	if err != nil {
		return err
	}
	cfg := store.Get()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opts := sim.Options{URL: cmp.Or(*url, config.GSIURI(cfg.Listen)), Token: cfg.Token, Speed: *speed}
	fmt.Printf("replaying %s into %s at %gx speed (Ctrl+C to stop)\n", fs.Arg(0), opts.URL, *speed)
	err = sim.Replay(ctx, fs.Arg(0), opts, func(n int) {
		if n%500 == 0 {
			fmt.Printf("  %d updates sent\n", n)
		}
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// reorderFlags moves flags ahead of positional arguments, since the flag package stops at
// the first positional one and `replay FILE -speed 10` is the natural way to type it.
func reorderFlags(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return append(flags, positional...)
}
