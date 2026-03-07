package ess

import (
	"log/slog"

	"github.com/raterudder/raterudder/pkg/log"
)

func init() {
	log.SetDefaultLogLevel(slog.LevelError)
}
