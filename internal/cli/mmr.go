package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/model"
	"gourdian/internal/stats"
)

func mmrCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: gourdian mmr 2450 [note]")
	}
	mmr, err := strconv.Atoi(args[0])
	if err != nil || mmr <= 0 || mmr > 20000 {
		return fmt.Errorf("%q is not an MMR value", args[0])
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	st, err := stats.Open(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.AppendMMR(model.MMREntry{Date: time.Now(), MMR: mmr, Note: strings.Join(args[1:], " ")}); err != nil {
		return err
	}
	fmt.Println("logged MMR", mmr, "in", st.Dir())
	return nil
}
