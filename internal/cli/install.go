package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"gourdian/internal/config"
	"gourdian/internal/install"
)

func installCmd(args []string, remove bool) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	dota := fs.String("dota", "", `path to the "dota 2 beta" folder (auto-detected by default)`)
	quiet := fs.Bool("quiet", false, "print nothing (used by the uninstaller)")
	fs.Parse(args)
	out := io.Writer(os.Stdout)
	if *quiet {
		out = io.Discard
	}

	store, _, err := config.OpenDefault()
	if err != nil {
		return err
	}
	cfg := store.Get()
	dirs := install.FindDota()
	if *dota != "" {
		dirs = []string{install.WSLPath(*dota)}
	}
	if len(dirs) == 0 {
		//lint:ignore ST1005 printed to the player as a sentence, and it starts with a name
		return errors.New(`Dota 2 not found; pass -dota "<Steam library>/steamapps/common/dota 2 beta"`)
	}
	for _, d := range dirs {
		if remove {
			path, err := install.Remove(d)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, "removed", path)
			continue
		}
		path, err := install.Write(d, config.GSIURI(cfg.Listen), cfg.Token)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "wrote", path)
	}
	if !remove {
		fmt.Fprintln(out, "\nRestart Dota 2 if it's running. Its launch options must include -gamestateintegration.")
	}
	return nil
}
