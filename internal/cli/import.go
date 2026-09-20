package cli

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"gourdian/internal/config"
	"gourdian/internal/dotadata"
	"gourdian/internal/matchdata"
	"gourdian/internal/stats"
)

func importCmd(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	n := fs.Int("n", 50, "how many recent matches to look at (up to 100)")
	account := fs.String("account", "", "Steam account id (default: the one Dota reported)")
	fs.Parse(args)

	store, dir, err := config.OpenDefault()
	if err != nil {
		return err
	}
	accountID := cmp.Or(*account, store.Settings().AccountID)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	data := dotadata.New(filepath.Join(dir, "cache"), log)
	data.Start(ctx)
	st, err := stats.Open(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	svc := matchdata.Service{Data: data, Stats: st, Log: log}
	fmt.Printf("importing up to %d matches for account %s (about one per second)\n", min(max(*n, 1), 100), accountID)
	added, err := svc.Import(ctx, accountID, min(max(*n, 1), 100), func(p matchdata.ImportProgress) {
		fmt.Printf("\r  %d/%d checked, %d added", p.Done, p.Total, p.Added)
	})
	fmt.Println()
	if err != nil {
		return err
	}
	fmt.Printf("added %d matches to %s\n", added, filepath.Join(dir, "stats"))
	return nil
}
