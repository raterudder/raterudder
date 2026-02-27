package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/types"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrSiteNotFound = errors.New("site not found")
)

// Database defines the interface for persisting data and retrieving settings.
type Database interface {
	// Settings
	GetSettings(ctx context.Context, siteID string) (types.Settings, int, error)
	SetSettings(ctx context.Context, siteID string, settings types.Settings, version int) error

	// Data Persistence
	// UpsertPrice adds or updates a price record.
	UpsertPrice(ctx context.Context, siteID string, price types.Price, version int) error
	InsertAction(ctx context.Context, siteID string, action types.Action) error
	UpsertEnergyHistory(ctx context.Context, siteID string, stats types.EnergyStats, version int) error
	UpdateESSMockState(ctx context.Context, siteID string, state types.ESSMockState) error
	GetESSMockState(ctx context.Context, siteID string) (types.ESSMockState, error)

	// History
	GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error)
	GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error)
	GetLatestAction(ctx context.Context, siteID string) (*types.Action, error)
	GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.EnergyStats, error)
	GetLatestEnergyHistoryTime(ctx context.Context, siteID string) (time.Time, int, error)
	GetLatestPriceHistoryTime(ctx context.Context, siteID string) (time.Time, int, error)

	// Sites & Users
	GetSite(ctx context.Context, siteID string) (types.Site, error)
	ListSites(ctx context.Context) ([]types.Site, error)
	CreateSite(ctx context.Context, siteID string, site types.Site) error
	UpdateSite(ctx context.Context, siteID string, site types.Site) error
	GetUser(ctx context.Context, userID string) (types.User, error)
	CreateUser(ctx context.Context, user types.User) error
	UpdateUser(ctx context.Context, user types.User) error

	// Lifecycle
	Close() error
}

// Configured sets up the Storage provider based on flags.
func Configured() Database {
	provider := lflag.String("storage-provider", "firestore", "Storage provider to use (available: firestore)")

	var p struct{ Database }

	fs := configuredFirestore()

	lflag.Do(func() {
		switch *provider {
		case "firestore":
			if err := fs.Validate(); err != nil {
				panic(fmt.Sprintf("firestore validation failed: %v", err))
			}
			p.Database = fs
			if err := fs.Init(context.Background()); err != nil {
				panic(fmt.Sprintf("firestore init failed: %v", err))
			}
		default:
			panic(fmt.Sprintf("unknown storage provider: %s", *provider))
		}
	})

	return &p
}
