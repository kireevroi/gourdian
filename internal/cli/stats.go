package cli

import (
	"fmt"

	"gourdian/internal/config"
	"gourdian/internal/model"
	"gourdian/internal/stats"
)

func statsCmd() error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	st, err := stats.Open(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	matches, err := st.Matches()
	if err != nil {
		return err
	}
	fmt.Println("CSV files:", st.Dir())
	var real []model.MatchSummary
	for _, m := range matches {
		if m.Real() {
			real = append(real, m)
		}
	}
	fmt.Printf("%d matches recorded (%d simulated or practice)\n", len(matches), len(matches)-len(real))
	if len(real) == 0 && len(matches) > 0 {
		fmt.Println("No real matches yet, so these numbers come from simulated and practice ones:")
		real = matches
	}
	for _, window := range []struct {
		label    string
		from, to int
	}{{"last 10", len(real) - 10, len(real)}, {"10 before", len(real) - 20, len(real) - 10}} {
		part := real[max(window.from, 0):max(window.to, 0)]
		if len(part) == 0 {
			continue
		}
		var wins, deaths, gpm, lh10, lh10n int
		for _, m := range part {
			if m.Result == "win" {
				wins++
			}
			deaths += m.Deaths
			gpm += m.GPM
			if lh, ok := m.LastHitsAt["10:00"]; ok {
				lh10 += lh
				lh10n++
			}
		}
		n := len(part)
		fmt.Printf("  %-9s  %2d games  win %3.0f%%  deaths %4.1f  GPM %4d  LH@10 %s\n", window.label, n,
			100*float64(wins)/float64(n), float64(deaths)/float64(n), gpm/n, avgOrDash(lh10, lh10n))
	}
	if mmr, err := st.MMR(); err == nil && len(mmr) > 0 {
		first, last := mmr[0], mmr[len(mmr)-1]
		fmt.Printf("MMR: %d on %s -> %d on %s (%+d)\n", first.MMR, first.Date.Format("2006-01-02"), last.MMR,
			last.Date.Format("2006-01-02"), last.MMR-first.MMR)
	}
	if reviews, err := st.Reviews(); err == nil && len(reviews) > 0 {
		fmt.Println("Current focus:", reviews[len(reviews)-1].NextGameFocus)
	}
	fmt.Println("Charts: open the dashboard and click Stats (http://127.0.0.1:4570/stats.html)")
	return nil
}

func avgOrDash(sum, n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprint(sum / n)
}
