package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FirestoreProvider implements the Provider interface using Google Cloud Firestore.
// It persists settings, prices, and actions to Firestore collections.
type FirestoreProvider struct {
	client    *firestore.Client
	projectID string
	database  string
}

// configuredFirestore sets up the Firestore provider.
// It registers flags for configuration.
func configuredFirestore() *FirestoreProvider {
	projectID := lflag.String("firestore-project-id", "", "Google Cloud Project ID for Firestore")
	database := lflag.String("firestore-database", "", "Google Cloud Firestore Database")
	emulator := lflag.String("firestore-emulator", "", "Use Firestore emulator")

	f := &FirestoreProvider{}

	lflag.Do(func() {
		f.projectID = *projectID
		f.database = *database

		// set this because that's how firestore client expects it
		if *emulator != "" {
			os.Setenv("FIRESTORE_EMULATOR_HOST", *emulator)
		}
	})

	return f
}

// Validate checks if the provider is properly configured.
func (f *FirestoreProvider) Validate() error {
	// Project ID verification could be here, but we allow empty if inferred.
	return nil
}

// Init initializes the Firestore client.
// This must be called before using the provider methods.
func (f *FirestoreProvider) Init(ctx context.Context) error {
	projectID := f.projectID
	if projectID == "" {
		projectID = firestore.DetectProjectID
	}
	database := f.database
	if database == "" {
		database = firestore.DefaultDatabaseID
	}
	client, err := firestore.NewClientWithDatabase(ctx, projectID, database)
	if err != nil {
		return fmt.Errorf("failed to create firestore client (project=%s, database=%s): %w", projectID, database, err)
	}
	f.client = client
	return nil
}

// Close closes the Firestore client connection.
func (f *FirestoreProvider) Close() error {
	if f.client != nil {
		return f.client.Close()
	}
	return nil
}

func (f *FirestoreProvider) getCollection(siteID, name string) (*firestore.CollectionRef, error) {
	if siteID == "" {
		return nil, fmt.Errorf("siteID cannot be empty")
	}
	return f.client.Collection("sites").Doc(siteID).Collection(name), nil
}

// GetSettings retrieves the dynamic configuration from the "config/settings" document.
func (f *FirestoreProvider) GetSettings(ctx context.Context, siteID string) (types.Settings, int, error) {
	coll, err := f.getCollection(siteID, "config")
	if err != nil {
		return types.Settings{}, 0, err
	}
	doc, err := coll.Doc("settings").Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// Return default settings if not found
			return types.Settings{}, 0, nil
		}
		return types.Settings{}, 0, fmt.Errorf("failed to fetch settings doc: %w", err)
	}

	// Read version if available (default 0)
	var version int
	if v, err := doc.DataAt("version"); err == nil {
		if vInt, ok := v.(int64); ok {
			version = int(vInt)
		}
	}

	val, err := doc.DataAt("json")
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "settings doc missing json", slog.String("siteID", siteID))
		return types.Settings{}, 0, fmt.Errorf("settings document missing 'json' field: %w", err)
	}

	jsonStr, ok := val.(string)
	if !ok {
		log.Ctx(ctx).WarnContext(ctx, "settings doc json not string", slog.String("siteID", siteID))
		return types.Settings{}, 0, fmt.Errorf("settings 'json' field is not a string")
	}

	var s types.Settings
	if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal settings json", slog.String("siteID", siteID), slog.Any("err", err))
		return types.Settings{}, 0, fmt.Errorf("failed to unmarshal settings json: %w", err)
	}
	return s, version, nil
}

// SetSettings saves the dynamic configuration to the "config/settings" document.
// It stores the settings as a JSON string for portability.
func (f *FirestoreProvider) SetSettings(ctx context.Context, siteID string, settings types.Settings, version int) error {
	jsonBytes, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	coll, err := f.getCollection(siteID, "config")
	if err != nil {
		return err
	}
	_, err = coll.Doc("settings").Set(ctx, map[string]interface{}{
		"json":    string(jsonBytes),
		"version": version,
	})
	if err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}
	return nil
}

// InsertAction adds a new action record to the "actions" collection as a JSON blob.
// The document ID is the RFC3339 timestamp for efficient range queries.
func (f *FirestoreProvider) InsertAction(ctx context.Context, siteID string, action types.Action) error {
	jsonBytes, err := json.Marshal(action)
	if err != nil {
		return fmt.Errorf("failed to marshal action: %w", err)
	}

	coll, err := f.getCollection(siteID, "action_history")
	if err != nil {
		return err
	}
	// Use RFC3339 as document ID for lexicographic ordering and efficient range queries
	docID := action.Timestamp.UTC().Format(time.RFC3339)
	_, err = coll.Doc(docID).Set(ctx, map[string]interface{}{
		"json":      string(jsonBytes),
		"timestamp": action.Timestamp,
	})
	if err != nil {
		return fmt.Errorf("failed to insert action: %w", err)
	}
	return nil
}

// GetActionHistory retrieves action records within the specified time range.
// Uses document ID range queries for efficient filtering without reading all documents.
func (f *FirestoreProvider) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	startDocID := start.UTC().Format(time.RFC3339)
	endDocID := end.UTC().Format(time.RFC3339)

	coll, err := f.getCollection(siteID, "action_history")
	if err != nil {
		return nil, err
	}
	iter := coll.
		Where(firestore.DocumentID, ">=", coll.Doc(startDocID)).
		Where(firestore.DocumentID, "<", coll.Doc(endDocID)).
		OrderBy(firestore.DocumentID, firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var actions []types.Action
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating actions: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "action doc missing json", slog.String("actionID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("action document %s missing 'json' field: %w", doc.Ref.ID, err)
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "action doc json not string", slog.String("actionID", doc.Ref.ID), slog.String("siteID", siteID))
			return nil, fmt.Errorf("action document %s 'json' field is not string", doc.Ref.ID)
		}

		var a types.Action
		if err := json.Unmarshal([]byte(jsonStr), &a); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal action", slog.String("actionID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("failed to unmarshal action (id=%s): %w", doc.Ref.ID, err)
		}
		actions = append(actions, a)
	}
	return actions, nil
}

// GetLatestAction retrieves the most recent action record. Will return nil if no actions found.
func (f *FirestoreProvider) GetLatestAction(ctx context.Context, siteID string) (*types.Action, error) {
	coll, err := f.getCollection(siteID, "action_history")
	if err != nil {
		return nil, err
	}

	iter := coll.
		OrderBy("timestamp", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, nil // No actions found
	}
	if err != nil {
		return nil, fmt.Errorf("error getting latest action: %w", err)
	}

	val, err := doc.DataAt("json")
	if err != nil {
		return nil, fmt.Errorf("action doc %s missing 'json': %w", doc.Ref.ID, err)
	}

	jsonStr, ok := val.(string)
	if !ok {
		return nil, fmt.Errorf("action doc %s 'json' field is not string", doc.Ref.ID)
	}

	var a types.Action
	if err := json.Unmarshal([]byte(jsonStr), &a); err != nil {
		return nil, fmt.Errorf("failed to unmarshal action: %w", err)
	}

	return &a, nil
}

// UpsertEnergyHistories adds or updates multiple energy history records in the "energy_history" collection.
func (f *FirestoreProvider) UpsertEnergyHistories(ctx context.Context, siteID string, stats []types.EnergyStats, version int) error {
	if len(stats) == 0 {
		return nil
	}

	coll, err := f.getCollection(siteID, "energy_history")
	if err != nil {
		return err
	}

	// For a single item, use direct Set to avoid batch overhead
	if len(stats) == 1 {
		s := stats[0]
		if s.TSHourStart.IsZero() {
			return fmt.Errorf("energy stats missing tsHourStart")
		}
		jsonBytes, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("failed to marshal energy stats: %w", err)
		}
		docID := s.TSHourStart.UTC().Format(time.RFC3339)
		_, err = coll.Doc(docID).Set(ctx, map[string]interface{}{
			"json":      string(jsonBytes),
			"timestamp": s.TSHourStart,
			"version":   version,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert energy history: %w", err)
		}
		return nil
	}

	// For multiple items, use batches chunked by 100
	const maxBatchSize = 100
	var batch *firestore.WriteBatch
	count := 0

	for _, s := range stats {
		if s.TSHourStart.IsZero() {
			return fmt.Errorf("energy stats missing tsHourStart")
		}

		if count == 0 {
			batch = f.client.Batch()
		}

		jsonBytes, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("failed to marshal energy stats: %w", err)
		}

		docID := s.TSHourStart.UTC().Format(time.RFC3339)
		ref := coll.Doc(docID)
		batch.Set(ref, map[string]interface{}{
			"json":      string(jsonBytes),
			"timestamp": s.TSHourStart,
			"version":   version,
		})

		count++
		if count >= maxBatchSize {
			if _, err := batch.Commit(ctx); err != nil {
				return fmt.Errorf("failed to commit energy history batch: %w", err)
			}
			count = 0
		}
	}

	// Commit any remaining items
	if count > 0 {
		if _, err := batch.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit remaining energy history batch: %w", err)
		}
	}

	return nil
}

// GetEnergyHistory retrieves energy history records within the specified time range.
func (f *FirestoreProvider) GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.EnergyStats, error) {
	startDocID := start.Truncate(time.Hour).UTC().Format(time.RFC3339)
	endDocID := end.Truncate(time.Hour).UTC().Format(time.RFC3339)

	coll, err := f.getCollection(siteID, "energy_history")
	if err != nil {
		return nil, err
	}
	iter := coll.
		Where(firestore.DocumentID, ">=", coll.Doc(startDocID)).
		Where(firestore.DocumentID, "<", coll.Doc(endDocID)).
		OrderBy(firestore.DocumentID, firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var allStats []types.EnergyStats
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating hourly energy history: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "energy stats doc missing json", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("energy stats doc %s missing 'json' field: %w", doc.Ref.ID, err)
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "energy stats doc json not string", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID))
			return nil, fmt.Errorf("energy stats doc %s 'json' field is not string", doc.Ref.ID)
		}

		var s types.EnergyStats
		if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal energy stats", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("failed to unmarshal energy stats (id=%s): %w", doc.Ref.ID, err)
		}
		allStats = append(allStats, s)
	}
	return allStats, nil
}

// GetLatestEnergyHistoryTime retrieves the timestamp of the last stored energy history record.
func (f *FirestoreProvider) GetLatestEnergyHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	coll, err := f.getCollection(siteID, "energy_history")
	if err != nil {
		return time.Time{}, 0, err
	}
	iter := coll.
		OrderBy("timestamp", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		return time.Time{}, 0, nil
	}
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("failed to get latest energy history doc: %w", err)
	}

	ts, err := time.Parse(time.RFC3339, doc.Ref.ID)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid energy history doc id %s: %w", doc.Ref.ID, err)
	}

	// Read version if available (default 0)
	var version int
	if v, err := doc.DataAt("version"); err == nil {
		if vInt, ok := v.(int64); ok {
			version = int(vInt)
		}
	}

	return ts, version, nil
}

// GetSite retrieves a site from the "sites" collection.
func (f *FirestoreProvider) GetSite(ctx context.Context, siteID string) (types.Site, error) {
	doc, err := f.client.Collection("sites").Doc(siteID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return types.Site{}, fmt.Errorf("%w: %s", ErrSiteNotFound, siteID)
		}
		return types.Site{}, fmt.Errorf("failed to get site %s: %w", siteID, err)
	}

	val, err := doc.DataAt("json")
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "site doc missing json", slog.String("siteID", siteID), slog.Any("err", err))
		return types.Site{}, fmt.Errorf("site %s missing json: %w", siteID, err)
	}
	jsonStr, ok := val.(string)
	if !ok {
		log.Ctx(ctx).WarnContext(ctx, "site doc json not string", slog.String("siteID", siteID))
		return types.Site{}, fmt.Errorf("site %s json not string", siteID)
	}

	var site types.Site
	if err := json.Unmarshal([]byte(jsonStr), &site); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal site", slog.String("siteID", siteID), slog.Any("err", err))
		return types.Site{}, fmt.Errorf("failed to unmarshal site %s: %w", siteID, err)
	}
	return site, nil
}

// ListSites retrieves all sites from the "sites" collection.
func (f *FirestoreProvider) ListSites(ctx context.Context) ([]types.Site, error) {
	iter := f.client.Collection("sites").Documents(ctx)
	defer iter.Stop()

	var sites []types.Site
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating sites: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "site doc missing json", slog.String("siteID", doc.Ref.ID))
			// Skip malformed documents
			continue
		}
		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "site doc json not string", slog.String("siteID", doc.Ref.ID))
			continue
		}

		var site types.Site
		if err := json.Unmarshal([]byte(jsonStr), &site); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal site", slog.String("siteID", doc.Ref.ID), slog.Any("err", err))
			// Skip malformed JSON
			continue
		}
		sites = append(sites, site)
	}
	return sites, nil
}

// GetUser retrieves a user from the "users" collection.
func (f *FirestoreProvider) GetUser(ctx context.Context, userID string) (types.User, error) {
	doc, err := f.client.Collection("users").Doc(userID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return types.User{}, fmt.Errorf("%w: %s", ErrUserNotFound, userID)
		}
		return types.User{}, fmt.Errorf("failed to get user %s: %w", userID, err)
	}

	val, err := doc.DataAt("json")
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "user doc missing json", slog.String("userID", userID))
		return types.User{}, fmt.Errorf("user %s missing json: %w", userID, err)
	}
	jsonStr, ok := val.(string)
	if !ok {
		log.Ctx(ctx).WarnContext(ctx, "user doc json not string", slog.String("userID", userID))
		return types.User{}, fmt.Errorf("user %s json not string", userID)
	}

	var user types.User
	if err := json.Unmarshal([]byte(jsonStr), &user); err != nil {
		return types.User{}, fmt.Errorf("failed to unmarshal user %s: %w", userID, err)
	}
	return user, nil
}

// UpsertPrices adds or updates multiple price records in the "price_history" sub-collection of the site.
func (f *FirestoreProvider) UpsertPrices(ctx context.Context, siteID string, prices []types.Price, version int) error {
	if len(prices) == 0 {
		return nil
	}

	coll, err := f.getCollection(siteID, "price_history")
	if err != nil {
		return err
	}

	// For a single item, use direct Set to avoid batch overhead
	if len(prices) == 1 {
		p := prices[0]
		jsonBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("failed to marshal price: %w", err)
		}
		docID := p.TSStart.UTC().Format(time.RFC3339)
		_, err = coll.Doc(docID).Set(ctx, map[string]interface{}{
			"json":      string(jsonBytes),
			"timestamp": p.TSStart,
			"version":   version,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert price: %w", err)
		}
		return nil
	}

	// For multiple items, use batches chunked by 100
	const maxBatchSize = 100
	var batch *firestore.WriteBatch
	count := 0

	for _, p := range prices {
		if count == 0 {
			batch = f.client.Batch()
		}

		jsonBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("failed to marshal price: %w", err)
		}

		docID := p.TSStart.UTC().Format(time.RFC3339)
		ref := coll.Doc(docID)
		batch.Set(ref, map[string]interface{}{
			"json":      string(jsonBytes),
			"timestamp": p.TSStart,
			"version":   version,
		})

		count++
		if count >= maxBatchSize {
			if _, err := batch.Commit(ctx); err != nil {
				return fmt.Errorf("failed to commit price batch: %w", err)
			}
			count = 0
		}
	}

	// Commit any remaining items
	if count > 0 {
		if _, err := batch.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit remaining price batch: %w", err)
		}
	}

	return nil
}

// GetPriceHistory retrieves price records within the specified time range for a site.
// Uses document ID range queries for efficient filtering.
func (f *FirestoreProvider) GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error) {
	startDocID := start.UTC().Format(time.RFC3339)
	endDocID := end.UTC().Format(time.RFC3339)

	coll, err := f.getCollection(siteID, "price_history")
	if err != nil {
		return nil, err
	}

	iter := coll.
		Where(firestore.DocumentID, ">=", coll.Doc(startDocID)).
		Where(firestore.DocumentID, "<", coll.Doc(endDocID)).
		OrderBy(firestore.DocumentID, firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var prices []types.Price
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating prices: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "price doc missing json", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("price document %s missing 'json' field: %w", doc.Ref.ID, err)
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "price doc json not string", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID))
			return nil, fmt.Errorf("price document %s 'json' field is not string", doc.Ref.ID)
		}

		var p types.Price
		if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal price", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("failed to unmarshal price (id=%s): %w", doc.Ref.ID, err)
		}
		prices = append(prices, p)
	}
	return prices, nil
}

// GetLatestPriceHistoryTime retrieves the timestamp of the last stored price record for a site.
func (f *FirestoreProvider) GetLatestPriceHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	coll, err := f.getCollection(siteID, "price_history")
	if err != nil {
		return time.Time{}, 0, err
	}

	// firestore automatically creates indexes for top-level fields
	iter := coll.
		OrderBy("timestamp", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		return time.Time{}, 0, nil
	}
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("failed to get latest price doc: %w", err)
	}

	ts, err := time.Parse(time.RFC3339, doc.Ref.ID)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid price doc id %s: %w", doc.Ref.ID, err)
	}

	// Read version if available (default 0)
	var version int
	if v, err := doc.DataAt("version"); err == nil {
		if vInt, ok := v.(int64); ok {
			version = int(vInt)
		}
	}

	return ts, version, nil
}

// CreateSite creates a new site document in the "sites" collection.
// It fails atomically if a document with the same siteID already exists,
// preventing race conditions.
func (f *FirestoreProvider) CreateSite(ctx context.Context, siteID string, site types.Site) error {
	siteJSON, err := json.Marshal(site)
	if err != nil {
		return fmt.Errorf("failed to marshal site %s: %w", siteID, err)
	}
	_, err = f.client.Collection("sites").Doc(siteID).Create(ctx, map[string]interface{}{
		"json": string(siteJSON),
	})
	if err != nil {
		return fmt.Errorf("failed to create site %s: %w", siteID, err)
	}
	return nil
}

// UpdateSite updates a site document in the "sites" collection.
func (f *FirestoreProvider) UpdateSite(ctx context.Context, siteID string, site types.Site) error {
	siteJSON, err := json.Marshal(site)
	if err != nil {
		return fmt.Errorf("failed to marshal site %s: %w", siteID, err)
	}
	_, err = f.client.Collection("sites").Doc(siteID).Set(ctx, map[string]interface{}{
		"json": string(siteJSON),
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("failed to update site %s: %w", siteID, err)
	}
	return nil
}

// CreateUser creates a new user document in the "users" collection.
func (f *FirestoreProvider) CreateUser(ctx context.Context, user types.User) error {
	userJSON, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user %s: %w", user.ID, err)
	}
	_, err = f.client.Collection("users").Doc(user.ID).Create(ctx, map[string]interface{}{
		"json": string(userJSON),
	})
	if err != nil {
		return fmt.Errorf("failed to create user %s: %w", user.ID, err)
	}
	return nil
}

// UpdateUser updates an existing user document in the "users" collection.
func (f *FirestoreProvider) UpdateUser(ctx context.Context, user types.User) error {
	userJSON, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user %s: %w", user.ID, err)
	}
	_, err = f.client.Collection("users").Doc(user.ID).Set(ctx, map[string]interface{}{
		"json": string(userJSON),
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("failed to update user %s: %w", user.ID, err)
	}
	return nil
}

// UpdateESSMockState saves the internal state of a mock ESS provider.

func (f *FirestoreProvider) UpdateESSMockState(ctx context.Context, siteID string, state types.ESSMockState) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal mock state %s: %w", siteID, err)
	}

	coll, err := f.getCollection(siteID, "mocks")
	if err != nil {
		return err
	}
	_, err = coll.Doc("mock_ess").Set(ctx, map[string]interface{}{
		"json": string(stateJSON),
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("failed to save mock state: %w", err)
	}
	return nil
}

// GetESSMockState retrieves the internal state of a mock ESS provider.
func (f *FirestoreProvider) GetESSMockState(ctx context.Context, siteID string) (types.ESSMockState, error) {
	coll, err := f.getCollection(siteID, "mocks")
	if err != nil {
		return types.ESSMockState{}, err
	}
	doc, err := coll.Doc("mock_ess").Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return types.ESSMockState{}, nil
		}
		return types.ESSMockState{}, fmt.Errorf("failed to fetch mock state: %w", err)
	}
	val, err := doc.DataAt("json")
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "mock state doc missing json", slog.String("siteID", siteID))
		return types.ESSMockState{}, fmt.Errorf("mock state %s missing json: %w", siteID, err)
	}
	jsonStr, ok := val.(string)
	if !ok {
		log.Ctx(ctx).WarnContext(ctx, "mock state doc json not string", slog.String("siteID", siteID))
		return types.ESSMockState{}, fmt.Errorf("mock state %s json not string", siteID)
	}

	var state types.ESSMockState
	if err := json.Unmarshal([]byte(jsonStr), &state); err != nil {
		return types.ESSMockState{}, fmt.Errorf("failed to unmarshal mock state %s: %w", siteID, err)
	}
	return state, nil
}
