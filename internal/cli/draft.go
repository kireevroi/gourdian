package cli

import (
	"context"
	"flag"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/dotadata"
	"gourdian/internal/screen"
)

// draftCmd photographs the screen while Dota is drafting and says which hero portraits it can
// make out. It is how the bar is checked against a real game rather than against a picture
// the tests paint, and how a player shows what went wrong when nothing is recognised.
func draftCmd(args []string) error {
	fs := flag.NewFlagSet("draft", flag.ContinueOnError)
	out := fs.String("out", "draft-frames", "folder to save the pictures in")
	every := fs.Duration("every", 2*time.Second, "how often to look")
	forHow := fs.Duration("for", 30*time.Second, "how long to keep looking")
	quiet := fs.Bool("quiet", false, "don't save the pictures, just say what was found")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if screen.Wayland() {
		return fmt.Errorf("this is a Wayland session, where a program can't read the screen; " +
			"log in with X11 (or Xorg) to use this")
	}
	size, err := screen.Size()
	if err != nil {
		return err
	}
	table, names, err := heroTable()
	if err != nil {
		return err
	}
	fmt.Printf("screen %dx%d, %d hero portraits known. Switch to Dota; looking every %s for %s.\n",
		size.Dx(), size.Dy(), len(table), *every, *forHow)
	if !*quiet {
		if err := os.MkdirAll(*out, 0o755); err != nil {
			return err
		}
	}

	deadline := time.Now().Add(*forHow)
	for shot := 1; time.Now().Before(deadline); shot++ {
		time.Sleep(*every)
		img, err := screen.Grab(size)
		if err != nil {
			return err
		}
		if !*quiet {
			name := filepath.Join(*out, fmt.Sprintf("frame%02d.png", shot))
			f, err := os.Create(name)
			if err != nil {
				return err
			}
			err = png.Encode(f, img)
			if closeErr := f.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				return err
			}
		}
		started := time.Now()
		found := screen.FindAny(img, table)
		sort.Slice(found, func(i, j int) bool { return found[i].Cell.Min.X < found[j].Cell.Min.X })
		fmt.Printf("frame %2d: %d portraits in %s", shot, len(found), time.Since(started).Round(time.Millisecond))
		for _, s := range found {
			fmt.Printf("\n   %-22s at %d,%d %dx%d", names[s.Hero], s.Cell.Min.X, s.Cell.Min.Y, s.Cell.Dx(), s.Cell.Dy())
		}
		fmt.Println()
	}
	if !*quiet {
		fmt.Printf("pictures saved in %s\n", *out)
	}
	return nil
}

// heroTable is what every hero's portrait looks like, numbered the way the game state and
// OpenDota both number them, and their names for printing.
func heroTable() (screen.Table, map[int]string, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, nil, err
	}
	data := dotadata.New(filepath.Join(dir, "cache"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data.Start(ctx)
	data.WaitReady(ctx)
	heroes := data.Heroes()
	if len(heroes) == 0 {
		return nil, nil, fmt.Errorf("the hero list isn't here yet; start the trainer once so it can fetch it, then try again")
	}
	ids, names := map[string]int{}, map[int]string{}
	for _, h := range heroes {
		ids[h.Name] = h.ID
		names[h.ID] = h.LocalizedName
	}
	return screen.TableFor(ids), names, nil
}
