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

	// Rocky Mountain Power uses Mountain Time
	mtLocation = func() *time.Location {
		loc, err := time.LoadLocation("America/Denver")
		if err != nil {
			panic(fmt.Errorf("failed to load mountain time location: %w", err))
		}
		return loc
	}()

	// Salt River Project uses Mountain Standard Time (no DST)
	mstLocation = func() *time.Location {
		loc, err := time.LoadLocation("America/Phoenix")
		if err != nil {
			panic(fmt.Errorf("failed to load Arizona time location: %w", err))
		}
		return loc
	}()

	// Hawaiian Electric uses Hawaii Time (no DST)
	hstLocation = func() *time.Location {
		loc, err := time.LoadLocation("Pacific/Honolulu")
		if err != nil {
			panic(fmt.Errorf("failed to load Hawaii time location: %w", err))
		}
		return loc
	}()

	bneLocation = func() *time.Location {
		loc, err := time.LoadLocation("Australia/Brisbane")
		if err != nil {
			panic(fmt.Errorf("failed to load Brisbane time location: %w", err))
		}
		return loc
	}()

	melLocation = func() *time.Location {
		loc, err := time.LoadLocation("Australia/Melbourne")
		if err != nil {
			panic(fmt.Errorf("failed to load Melbourne time location: %w", err))
		}
		return loc
	}()

	sydLocation = func() *time.Location {
		loc, err := time.LoadLocation("Australia/Sydney")
		if err != nil {
			panic(fmt.Errorf("failed to load Sydney time location: %w", err))
		}
		return loc
	}()
)
