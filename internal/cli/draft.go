package cli

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
	bar := screen.Predict(size)
	fmt.Printf("expecting the portraits at %+v and %+v\n", bar.Left, bar.Right)
	var seen screen.Reading
	searched := false
	for shot := 1; time.Now().Before(deadline); shot++ {
		time.Sleep(*every)
		img, err := screen.Grab(size)
		if err != nil {
			return err
		}
		if !*quiet {
			if err := savePNG(filepath.Join(*out, fmt.Sprintf("frame%02d.png", shot)), img); err != nil {
				return err
			}
		}
		fresh := seen.Add(img, bar, table)
		// Dota's interface can be scaled by hand, and then the guess reads nothing. Look for
		// the bar properly, once, before giving up on it.
		if seen.Settled() == 0 && !searched {
			searched = true
			if found := screen.FindAny(img, table); len(found) > 0 {
				fmt.Printf("frame %2d: the guess found nothing, but a sweep found %d portraits:\n", shot, len(found))
				for _, f := range found {
					fmt.Printf("   %-22s at %d,%d %dx%d\n", names[f.Hero], f.Cell.Min.X, f.Cell.Min.Y, f.Cell.Dx(), f.Cell.Dy())
				}
			}
		}
		fmt.Printf("frame %2d: %d of 10 known%s\n", shot, seen.Settled(), more(fresh))
		if fresh > 0 || seen.Settled() == 2*screen.Slots {
			printBoard(seen.Heroes(), names)
		}
		if seen.Settled() == 2*screen.Slots {
			fmt.Println("the whole bar is read; stopping early")
			break
		}
	}
	if !*quiet {
		fmt.Printf("pictures saved in %s\n", *out)
	}
	return nil
}

func more(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(", %d just now", n)
}

func printBoard(heroes [2 * screen.Slots]int, names map[int]string) {
	for _, side := range []struct {
		name string
		from int
	}{{"yours ", 0}, {"theirs", screen.Slots}} {
		fmt.Printf("   %s:", side.name)
		for _, id := range heroes[side.from : side.from+screen.Slots] {
			if id == 0 {
				fmt.Printf(" %-18s", "?")
				continue
			}
			fmt.Printf(" %-18s", names[id])
		}
		fmt.Println()
	}
}

func savePNG(name string, img image.Image) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	err = png.Encode(f, img)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
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
