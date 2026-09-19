package overlay

import "time"

const (
	dashboardTitle = "Gourdian ·" // every dashboard page's title starts with this
	// opening is how long a window gets to appear. Until it does its title is still the URL,
	// so a second press would find nothing and open the dashboard twice.
	opening = 10 * time.Second
)
