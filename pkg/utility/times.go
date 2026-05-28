package utility

import (
	"fmt"
	"time"
)

var (
	// PJM uses Eastern Time
	// MISO uses Eastern Time
	// Duke uses Eastern Time
	// Georgia Power uses Eastern Time
	etLocation = func() *time.Location {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			panic(fmt.Errorf("failed to load eastern time location: %w", err))
		}
		return loc
	}()

	// ComEd uses Central Time
	// Xcel uses Central Time
	ctLocation = func() *time.Location {
		loc, err := time.LoadLocation("America/Chicago")
		if err != nil {
			panic(fmt.Errorf("failed to load central time location: %w", err))
		}
		return loc
	}()

	// SCE uses Pacific Time
	// PGE uses Pacific Time
	ptLocation = func() *time.Location {
		loc, err := time.LoadLocation("America/Los_Angeles")
		if err != nil {
			panic(fmt.Errorf("failed to load pacific time location: %w", err))
		}
		return loc
	}()
)
