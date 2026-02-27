package ess

import (
	"log/slog"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage/storagemock"
)

type mockStorage = storagemock.MockDatabase

func init() {
	log.SetDefaultLogLevel(slog.LevelError)
}
