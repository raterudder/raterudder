package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
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

// FirestoreClient returns the underlying *firestore.Client.
func (f *FirestoreProvider) FirestoreClient() *firestore.Client {
	return f.client
}

// Ping checks if the Firestore client can connect to the database.
func (f *FirestoreProvider) Ping(ctx context.Context) error {
	if f.client == nil {
		return fmt.Errorf("firestore client not initialized")
	}
	// Use a simple operation to check connectivity
	_, err := f.client.Doc("health_check/non_existent").Get(ctx)
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("failed to ping firestore: %w", err)
	}
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
	_, err = coll.Doc("settings").Set(ctx, map[string]any{
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
	_, err = coll.Doc(docID).Set(ctx, map[string]any{
		"json":      string(jsonBytes),
		"timestamp": action.Timestamp,
	})
	if err != nil {
		return fmt.Errorf("failed to insert action: %w", err)
	}
	return nil
}

// GetActionHistory retrieves action records within the specified time range.
func (f *FirestoreProvider) GetActionHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Action, error) {
	coll, err := f.getCollection(siteID, "action_history")
	if err != nil {
		return nil, err
	}
	iter := coll.
		Where("timestamp", ">=", start).
		Where("timestamp", "<", end).
		OrderBy("timestamp", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var actions []types.Action
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
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
	if errors.Is(err, iterator.Done) {
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
func (f *FirestoreProvider) UpsertEnergyHistories(ctx context.Context, siteID string, stats []types.DailyEnergyStats, version int) error {
	if len(stats) == 0 {
		return nil
	}

	coll, err := f.getCollection(siteID, "energy_history")
	if err != nil {
		return err
	}

	for _, s := range stats {
		if s.TSDayStart.IsZero() {
			return fmt.Errorf("energy stats missing tsDayStart")
		}

		slices.SortFunc(s.Hourly, func(a, b types.EnergyStats) int {
			return a.TSHourStart.Compare(b.TSHourStart)
		})

		// Check for contiguous data and warn if not 'today'
		nowSite := time.Now().In(s.TSDayStart.Location())
		todayStart := time.Date(nowSite.Year(), nowSite.Month(), nowSite.Day(), 0, 0, 0, 0, nowSite.Location())
		if s.TSDayStart.Before(todayStart) {
			contiguous := true
			if len(s.Hourly) == 0 {
				contiguous = false
			} else {
				for i := 1; i < len(s.Hourly); i++ {
					if !s.Hourly[i].TSHourStart.Equal(s.Hourly[i-1].TSHourStart.Add(time.Hour)) {
						contiguous = false
						break
					}
				}
				// Also check if the day has roughly 24 hours of data.
				// A local day can have 23, 24, or 25 hours due to DST.
				// Just checking if we have at least 23 hours.
				if len(s.Hourly) < 23 {
					contiguous = false
				}
			}
			if !contiguous {
				log.Ctx(ctx).WarnContext(
					ctx,
					"non-contiguous energy data provided",
					slog.String("siteID", siteID),
					slog.Time("tsDayStart", s.TSDayStart),
					slog.Int("hours", len(s.Hourly)),
				)
			}
		}
	}

	// For a single item, use direct Set to avoid batch overhead
	if len(stats) == 1 {
		s := stats[0]
		s.TSDayStart = time.Date(s.TSDayStart.Year(), s.TSDayStart.Month(), s.TSDayStart.Day(), 0, 0, 0, 0, s.TSDayStart.Location())
		jsonBytes, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("failed to marshal energy stats: %w", err)
		}
		docID := s.TSDayStart.Format("2006-01-02")
		_, err = coll.Doc(docID).Set(ctx, map[string]any{
			"json":       string(jsonBytes),
			"tsDayStart": s.TSDayStart,
			"version":    version,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert energy history: %w", err)
		}
		return nil
	}

	// For multiple items, use BulkWriter
	bw := f.client.BulkWriter(ctx)
	jobs := make([]*firestore.BulkWriterJob, 0, len(stats))

	for _, s := range stats {
		s.TSDayStart = time.Date(s.TSDayStart.Year(), s.TSDayStart.Month(), s.TSDayStart.Day(), 0, 0, 0, 0, s.TSDayStart.Location())

		jsonBytes, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("failed to marshal energy stats: %w", err)
		}

		docID := s.TSDayStart.Format("2006-01-02")
		ref := coll.Doc(docID)

		job, err := bw.Set(ref, map[string]any{
			"json":       string(jsonBytes),
			"tsDayStart": s.TSDayStart,
			"version":    version,
		})
		if err != nil {
			return fmt.Errorf("failed to enqueue energy history: %w", err)
		}
		jobs = append(jobs, job)
	}

	bw.End()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return fmt.Errorf("failed to upsert energy history: %w", err)
		}
	}

	return nil
}

// UpsertWeather adds or updates multiple weather records.
func (f *FirestoreProvider) UpsertWeather(ctx context.Context, siteID string, weather []types.Weather, version int) error {
	if len(weather) == 0 {
		return nil
	}

	coll, err := f.getCollection(siteID, "weather")
	if err != nil {
		return err
	}

	if len(weather) == 1 {
		w := weather[0]
		jsonBytes, err := json.Marshal(w)
		if err != nil {
			return fmt.Errorf("failed to marshal weather: %w", err)
		}
		docID := w.TSDayStart.Format("2006-01-02")
		_, err = coll.Doc(docID).Set(ctx, map[string]any{
			"json":       string(jsonBytes),
			"tsDayStart": w.TSDayStart,
			"tsUpdated":  w.TSUpdated,
			"version":    version,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert weather: %w", err)
		}
		return nil
	}

	// For multiple items, use BulkWriter
	bw := f.client.BulkWriter(ctx)
	jobs := make([]*firestore.BulkWriterJob, 0, len(weather))

	for _, w := range weather {
		jsonBytes, err := json.Marshal(w)
		if err != nil {
			return fmt.Errorf("failed to marshal weather: %w", err)
		}

		docID := w.TSDayStart.Format("2006-01-02")
		ref := coll.Doc(docID)
		job, err := bw.Set(ref, map[string]any{
			"json":       string(jsonBytes),
			"tsDayStart": w.TSDayStart,
			"tsUpdated":  w.TSUpdated,
			"version":    version,
		})
		if err != nil {
			return fmt.Errorf("failed to enqueue weather: %w", err)
		}
		jobs = append(jobs, job)
	}

	bw.End()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return fmt.Errorf("failed to upsert weather: %w", err)
		}
	}

	return nil
}

// GetWeather retrieves weather records within the specified time range.
func (f *FirestoreProvider) GetWeather(ctx context.Context, siteID string, start, end time.Time) ([]types.Weather, error) {
	coll, err := f.getCollection(siteID, "weather")
	if err != nil {
		return nil, err
	}
	iter := coll.
		Where("tsDayStart", ">=", start).
		Where("tsDayStart", "<", end).
		OrderBy("tsDayStart", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var weather []types.Weather
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating weather: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "weather doc missing json", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			continue
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "weather doc json not string", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID))
			continue
		}

		var w types.Weather
		if err := json.Unmarshal([]byte(jsonStr), &w); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal weather", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			continue
		}

		weather = append(weather, w)
	}
	return weather, nil
}

// GetEnergyHistory retrieves energy history records within the specified time range.
func (f *FirestoreProvider) GetEnergyHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.DailyEnergyStats, error) {
	coll, err := f.getCollection(siteID, "energy_history")
	if err != nil {
		return nil, err
	}

	iter := coll.
		Where("tsDayStart", ">=", start).
		Where("tsDayStart", "<", end).
		OrderBy("tsDayStart", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var allStats []types.DailyEnergyStats
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating daily energy history: %w", err)
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

		var s types.DailyEnergyStats
		if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal daily energy stats", slog.String("docID", doc.Ref.ID), slog.String("siteID", siteID), slog.Any("err", err))
			return nil, fmt.Errorf("failed to unmarshal daily energy stats (id=%s): %w", doc.Ref.ID, err)
		}
		allStats = append(allStats, s)
	}

	// Lazy migration: if no daily stats found, check if any legacy hourly docs exist.
	if len(allStats) == 0 {
		oldIter := coll.Where("version", "<", 3).Limit(1).Documents(ctx)
		_, err := oldIter.Next()
		oldIter.Stop()
		if err == nil {
			log.Ctx(ctx).InfoContext(ctx, "migrating energy history schema (fallback check)", slog.String("siteID", siteID))
			if err := f.migrateEnergyHistory(ctx, siteID); err != nil {
				return nil, fmt.Errorf("failed to migrate energy history: %w", err)
			}
			// Retry the query after migration
			return f.GetEnergyHistory(ctx, siteID, start, end)
		} else if !errors.Is(err, iterator.Done) {
			return nil, fmt.Errorf("error checking for old energy history: %w", err)
		}
	}

	return allStats, nil
}

// migrateEnergyHistory fetches old hourly docs, aggregates them, saves new daily ones, and deletes the old ones.
func (f *FirestoreProvider) migrateEnergyHistory(ctx context.Context, siteID string) error {
	coll, err := f.getCollection(siteID, "energy_history")
	if err != nil {
		return err
	}

	// Query all hourly documents that haven't been migrated yet
	iter := coll.Where("version", "<", types.CurrentEnergyStatsVersion).Documents(ctx)
	defer iter.Stop()

	// Temporary map to group hourly stats into daily slices
	dailyGroups := make(map[string][]types.EnergyStats)
	var docsToDelete []*firestore.DocumentRef

	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("migrating: error fetching old doc: %w", err)
		}

		docsToDelete = append(docsToDelete, doc.Ref)

		val, err := doc.DataAt("json")
		if err != nil {
			return fmt.Errorf("migrating: error reading json of old doc %s: %w", doc.Ref.ID, err)
		}
		jsonStr, ok := val.(string)
		if !ok {
			return fmt.Errorf("migrating: error reading json of old doc %s: not a string", doc.Ref.ID)
		}
		var s types.EnergyStats
		if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
			return fmt.Errorf("migrating: error unmarshalling json of old doc %s: %w", doc.Ref.ID, err)
		}
		if s.TSHourStart.IsZero() {
			return fmt.Errorf("migrating: error reading TSHourStart of old doc %s: zero value", doc.Ref.ID)
		}

		// Use the timezone attached to the TSHourStart time to compute the day string.
		dayStr := s.TSHourStart.Format("2006-01-02")
		dailyGroups[dayStr] = append(dailyGroups[dayStr], s)
	}

	var dailyStats []types.DailyEnergyStats
	for dayStr, hourlyStats := range dailyGroups {
		// Just take the first element's timezone and reconstruct the day start
		loc := hourlyStats[0].TSHourStart.Location()
		parsed, err := time.ParseInLocation("2006-01-02", dayStr, loc)
		if err != nil {
			return fmt.Errorf("migrating: failed to parse day string %s: %w", dayStr, err)
		}

		daily := types.DailyEnergyStats{
			TSDayStart: parsed,
			Hourly:     hourlyStats,
		}
		dailyStats = append(dailyStats, daily)
	}

	// 1. Bulk write the new daily docs
	if err := f.UpsertEnergyHistories(ctx, siteID, dailyStats, types.CurrentEnergyStatsVersion); err != nil {
		return fmt.Errorf("migration: failed to save new daily docs: %w", err)
	}

	// 2. Delete the old hourly docs safely
	bw := f.client.BulkWriter(ctx)
	var deleteJobs []*firestore.BulkWriterJob
	for _, ref := range docsToDelete {
		job, err := bw.Delete(ref)
		if err == nil {
			deleteJobs = append(deleteJobs, job)
		}
	}
	bw.End()
	for _, job := range deleteJobs {
		if _, err := job.Results(); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to delete old energy doc during migration", slog.Any("err", err))
		}
	}

	return nil
}

// GetLatestEnergyHistoryTime retrieves the timestamp of the last stored energy
// history record.
func (f *FirestoreProvider) GetLatestEnergyHistoryTime(ctx context.Context, siteID string) (time.Time, int, error) {
	coll, err := f.getCollection(siteID, "energy_history")
	if err != nil {
		return time.Time{}, 0, err
	}
	iter := coll.
		OrderBy("tsDayStart", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if errors.Is(err, iterator.Done) {
		// instead look for them ordered by timestamp
		iter2 := coll.
			OrderBy("timestamp", firestore.Desc).
			Limit(1).
			Documents(ctx)
		defer iter2.Stop()
		doc2, err2 := iter2.Next()
		if errors.Is(err2, iterator.Done) {
			return time.Time{}, 0, nil
		}
		if err2 != nil {
			return time.Time{}, 0, fmt.Errorf("failed to get latest energy history doc (v2): %w", err2)
		}
		doc = doc2
		err = nil // found one in v2
	}
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("failed to get latest energy history doc (v3): %w", err)
	}

	// Read version if available (default 0)
	var version int
	if v, err := doc.DataAt("version"); err == nil {
		if vInt, ok := v.(int64); ok {
			version = int(vInt)
		}
	}
	// this is crucial because if we don't do this here then there might be a
	// call to UpsertEnergyHistories which will store the version 3 data and we
	// will never migrate old data.
	if version < 3 {
		log.Ctx(ctx).InfoContext(ctx, "migrating energy history schema synchronously (detecting old version in latest check)", slog.String("siteID", siteID))
		if err := f.migrateEnergyHistory(ctx, siteID); err != nil {
			return time.Time{}, 0, fmt.Errorf("failed to migrate energy history: %w", err)
		}
		return f.GetLatestEnergyHistoryTime(ctx, siteID)
	}

	ts, err := time.Parse("2006-01-02", doc.Ref.ID)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid energy history doc id %s: %w", doc.Ref.ID, err)
	}
	// If this doc is daily, the latest actual recorded hour needs to be extracted from JSON
	val, err := doc.DataAt("json")
	if err == nil {
		if jsonStr, ok := val.(string); ok {
			var s types.DailyEnergyStats
			if err := json.Unmarshal([]byte(jsonStr), &s); err == nil && len(s.Hourly) > 0 {
				latest := s.Hourly[0].TSHourStart
				for _, h := range s.Hourly {
					if h.TSHourStart.After(latest) {
						latest = h.TSHourStart
					}
				}
				return latest, version, nil
			}
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
		if errors.Is(err, iterator.Done) {
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
		_, err = coll.Doc(docID).Set(ctx, map[string]any{
			"json":      string(jsonBytes),
			"timestamp": p.TSStart,
			"version":   version,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert price: %w", err)
		}
		return nil
	}

	// For multiple items, use BulkWriter
	bw := f.client.BulkWriter(ctx)
	jobs := make([]*firestore.BulkWriterJob, 0, len(prices))

	for _, p := range prices {
		jsonBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("failed to marshal price: %w", err)
		}

		docID := p.TSStart.UTC().Format(time.RFC3339)
		ref := coll.Doc(docID)
		job, err := bw.Set(ref, map[string]any{
			"json":      string(jsonBytes),
			"timestamp": p.TSStart,
			"version":   version,
		})
		if err != nil {
			return fmt.Errorf("failed to enqueue price: %w", err)
		}
		jobs = append(jobs, job)
	}

	bw.End()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return fmt.Errorf("failed to upsert prices: %w", err)
		}
	}

	return nil
}

// GetPriceHistory retrieves price records within the specified time range for a site.
func (f *FirestoreProvider) GetPriceHistory(ctx context.Context, siteID string, start, end time.Time) ([]types.Price, error) {
	coll, err := f.getCollection(siteID, "price_history")
	if err != nil {
		return nil, err
	}

	iter := coll.
		Where("timestamp", ">=", start).
		Where("timestamp", "<", end).
		OrderBy("timestamp", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var prices []types.Price
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
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
	if errors.Is(err, iterator.Done) {
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

// GetLatestWeatherTime retrieves the timestamp of the last stored weather record for a site, along with its update time.
func (f *FirestoreProvider) GetLatestWeatherTime(ctx context.Context, siteID string) (time.Time, time.Time, int, error) {
	coll, err := f.getCollection(siteID, "weather")
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}

	iter := coll.
		OrderBy("tsDayStart", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if errors.Is(err, iterator.Done) {
		return time.Time{}, time.Time{}, 0, nil
	}
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("failed to get latest weather doc: %w", err)
	}

	ts, err := time.Parse("2006-01-02", doc.Ref.ID)
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("invalid weather doc id %s: %w", doc.Ref.ID, err)
	}

	// Read version if available (default 0)
	var version int
	if v, err := doc.DataAt("version"); err == nil {
		if vInt, ok := v.(int64); ok {
			version = int(vInt)
		}
	}

	// Read tsUpdated if available
	var tsUpdated time.Time
	if tUp, err := doc.DataAt("tsUpdated"); err == nil {
		if tTime, ok := tUp.(time.Time); ok {
			tsUpdated = tTime
		}
	}

	if tsUpdated.IsZero() {
		// Fallback to reading it from JSON
		if val, err := doc.DataAt("json"); err == nil {
			if jsonStr, ok := val.(string); ok {
				var w types.Weather
				if err := json.Unmarshal([]byte(jsonStr), &w); err == nil {
					tsUpdated = w.TSUpdated
				}
			}
		}
	}

	return ts, tsUpdated, version, nil
}

// CreateSite creates a new site document in the "sites" collection.
// It fails atomically if a document with the same siteID already exists,
// preventing race conditions.
func (f *FirestoreProvider) CreateSite(ctx context.Context, siteID string, site types.Site) error {
	siteJSON, err := json.Marshal(site)
	if err != nil {
		return fmt.Errorf("failed to marshal site %s: %w", siteID, err)
	}
	_, err = f.client.Collection("sites").Doc(siteID).Create(ctx, map[string]any{
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
	_, err = f.client.Collection("sites").Doc(siteID).Set(ctx, map[string]any{
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
	_, err = f.client.Collection("users").Doc(user.ID).Create(ctx, map[string]any{
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
	_, err = f.client.Collection("users").Doc(user.ID).Set(ctx, map[string]any{
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
	_, err = coll.Doc("mock_ess").Set(ctx, map[string]any{
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

// InsertFeedback adds a new feedback record to the "feedback" collection.
func (f *FirestoreProvider) InsertFeedback(ctx context.Context, feedback types.Feedback) error {
	jsonBytes, err := json.Marshal(feedback)
	if err != nil {
		return fmt.Errorf("failed to marshal feedback: %w", err)
	}

	coll := f.client.Collection("feedback")
	_, err = coll.Doc(feedback.ID).Set(ctx, map[string]any{
		"json": string(jsonBytes),
		"id":   feedback.ID,
	})
	if err != nil {
		return fmt.Errorf("failed to insert feedback: %w", err)
	}
	return nil
}

// ListFeedback retrieves feedback records, sorted by ID descending.
func (f *FirestoreProvider) ListFeedback(ctx context.Context, limit int, lastFeedbackID string) ([]types.Feedback, error) {
	coll := f.client.Collection("feedback")

	q := coll.OrderBy("id", firestore.Desc).Limit(limit)
	if lastFeedbackID != "" {
		q = q.StartAfter(lastFeedbackID)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var feedbacks []types.Feedback
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating feedback: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "feedback doc missing json", slog.String("docID", doc.Ref.ID), slog.Any("err", err))
			continue
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "feedback doc json not string", slog.String("docID", doc.Ref.ID))
			continue
		}

		var fb types.Feedback
		if err := json.Unmarshal([]byte(jsonStr), &fb); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal feedback", slog.String("docID", doc.Ref.ID), slog.Any("err", err))
			continue
		}
		feedbacks = append(feedbacks, fb)
	}

	return feedbacks, nil
}

// UpsertUtilityPrices adds or updates multiple price records for a utility.
func (f *FirestoreProvider) UpsertUtilityPrices(ctx context.Context, utilityID string, prices []types.PriceState, version int) error {
	if len(prices) == 0 {
		return nil
	}

	coll := f.client.Collection("utilities").Doc(utilityID).Collection("hourly_prices")

	if len(prices) == 1 {
		p := prices[0]
		jsonBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("failed to marshal utility price: %w", err)
		}
		docID := p.TSStart.UTC().Format(time.RFC3339)
		_, err = coll.Doc(docID).Set(ctx, map[string]any{
			"json":      string(jsonBytes),
			"timestamp": p.TSStart,
			"version":   version,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert utility price: %w", err)
		}
		return nil
	}

	bw := f.client.BulkWriter(ctx)
	jobs := make([]*firestore.BulkWriterJob, 0, len(prices))

	for _, p := range prices {
		jsonBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("failed to marshal utility price: %w", err)
		}

		docID := p.TSStart.UTC().Format(time.RFC3339)
		ref := coll.Doc(docID)
		job, err := bw.Set(ref, map[string]any{
			"json":      string(jsonBytes),
			"timestamp": p.TSStart,
			"version":   version,
		})
		if err != nil {
			return fmt.Errorf("failed to enqueue utility price: %w", err)
		}
		jobs = append(jobs, job)
	}

	bw.End()

	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return fmt.Errorf("failed to upsert utility prices: %w", err)
		}
	}

	return nil
}

// GetUtilityPrices retrieves price records within the specified time range for a utility.
func (f *FirestoreProvider) GetUtilityPrices(ctx context.Context, utilityID string, start, end time.Time) ([]types.PriceState, error) {
	coll := f.client.Collection("utilities").Doc(utilityID).Collection("hourly_prices")

	iter := coll.
		Where("timestamp", ">=", start).
		Where("timestamp", "<", end).
		OrderBy("timestamp", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var prices []types.PriceState
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating utility prices: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			continue
		}

		jsonStr, ok := val.(string)
		if !ok {
			continue
		}

		var p types.PriceState
		if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
			continue
		}
		prices = append(prices, p)
	}
	return prices, nil
}

// UpsertInterest adds or updates an interest submission record in the "interest" collection.
// It uses the email as the document ID to ensure one record per email.
func (f *FirestoreProvider) UpsertInterest(ctx context.Context, submission types.InterestSubmission) error {
	if submission.Email == "" {
		return fmt.Errorf("email cannot be empty for interest submission")
	}

	jsonBytes, err := json.Marshal(submission)
	if err != nil {
		return fmt.Errorf("failed to marshal interest submission: %w", err)
	}

	coll := f.client.Collection("interest")
	_, err = coll.Doc(submission.Email).Set(ctx, map[string]any{
		"json":      string(jsonBytes),
		"timestamp": submission.Timestamp,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert interest submission: %w", err)
	}
	return nil
}

// ListInterest retrieves interest submissions, sorted by timestamp descending.
func (f *FirestoreProvider) ListInterest(ctx context.Context, limit int) ([]types.InterestSubmission, error) {
	coll := f.client.Collection("interest")

	iter := coll.OrderBy("timestamp", firestore.Desc).Limit(limit).Documents(ctx)
	defer iter.Stop()

	var submissions []types.InterestSubmission
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating interest submissions: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "interest doc missing json", slog.String("docID", doc.Ref.ID), slog.Any("err", err))
			continue
		}

		jsonStr, ok := val.(string)
		if !ok {
			log.Ctx(ctx).WarnContext(ctx, "interest doc json not string", slog.String("docID", doc.Ref.ID))
			continue
		}

		var sub types.InterestSubmission
		if err := json.Unmarshal([]byte(jsonStr), &sub); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to unmarshal interest submission", slog.String("docID", doc.Ref.ID), slog.Any("err", err))
			continue
		}
		submissions = append(submissions, sub)
	}

	return submissions, nil
}

// DeleteInterest removes an interest submission record from the "interest" collection.
func (f *FirestoreProvider) DeleteInterest(ctx context.Context, email string) error {
	if email == "" {
		return fmt.Errorf("email cannot be empty for interest deletion")
	}

	coll := f.client.Collection("interest")
	_, err := coll.Doc(email).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete interest submission: %w", err)
	}
	return nil
}
