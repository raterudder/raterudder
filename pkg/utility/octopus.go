package utility

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/sync/singleflight"
)

// Octopus Energy REST API Reference:
// Octopus publishes public REST endpoints for product tariffs and unit rates:
// Base URL: https://api.octopus.energy/v1/products/
//
// Public Product Codes:
//   - Octopus Flux Import: FLUX-IMPORT-23-02-14
//   - Octopus Flux Export: FLUX-EXPORT-23-02-14
//   - Octopus Go: GO-VAR-22-10-14
//   - Intelligent Octopus Go: INTELLI-VAR-22-10-14
//   - Outgoing Octopus Export: OUTGOING-VAR-24-10-26
//   - Agile Outgoing Octopus: AGILE-OUTGOING-19-05-13
//
// Tariff Code Format: E-1R-{PRODUCT_CODE}-{REGION_LETTER} (e.g. E-1R-AGILE-OUTGOING-19-05-13-C for London)
// Example Tariff Unit Rates URL:
//   https://api.octopus.energy/v1/products/FLUX-IMPORT-23-02-14/electricity-tariffs/E-1R-FLUX-IMPORT-23-02-14-C/standard-unit-rates/
//   https://api.octopus.energy/v1/products/GO-VAR-22-10-14/electricity-tariffs/E-1R-GO-VAR-22-10-14-C/standard-unit-rates/
//   https://api.octopus.energy/v1/products/INTELLI-VAR-22-10-14/electricity-tariffs/E-1R-INTELLI-VAR-22-10-14-C/standard-unit-rates/
//   https://api.octopus.energy/v1/products/AGILE-OUTGOING-19-05-13/electricity-tariffs/E-1R-AGILE-OUTGOING-19-05-13-C/standard-unit-rates/

const (
	defaultOctopusAPIURL        = "https://api.octopus.energy/v1"
	octopusAgileOutgoingProduct = "AGILE-OUTGOING-19-05-13"
)

type octopusRateConfig struct {
	RegionCode string

	ImportPeakRate     float64
	ImportOffPeakRate  float64
	ImportStandardRate float64

	ExportPeakRate     float64
	ExportOffPeakRate  float64
	ExportStandardRate float64

	GoPeakRate    float64
	GoOffPeakRate float64

	IOGPeakRate    float64
	IOGOffPeakRate float64
}

// octopusRates stores current unit rates in base currency (£/kWh, e.g. 0.2450 = 24.5p/kWh)
// indexed by the 14 UK distribution network (DNO / PES) regions A through P.
var octopusRates = map[string]octopusRateConfig{
	"eastern": { // PES A / DNO 10
		RegionCode:         "A",
		ImportStandardRate: 0.2429,
		ImportPeakRate:     0.3401,
		ImportOffPeakRate:  0.1458,
		ExportStandardRate: 0.1011,
		ExportPeakRate:     0.2932,
		ExportOffPeakRate:  0.0499,
		GoPeakRate:         0.3471,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2896,
		IOGOffPeakRate:     0.0700,
	},
	"east_midlands": { // PES B / DNO 11
		RegionCode:         "B",
		ImportStandardRate: 0.2306,
		ImportPeakRate:     0.3229,
		ImportOffPeakRate:  0.1384,
		ExportStandardRate: 0.0976,
		ExportPeakRate:     0.2770,
		ExportOffPeakRate:  0.0434,
		GoPeakRate:         0.3303,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2777,
		IOGOffPeakRate:     0.0700,
	},
	"london": { // PES C / DNO 12
		RegionCode:         "C",
		ImportStandardRate: 0.2425,
		ImportPeakRate:     0.3396,
		ImportOffPeakRate:  0.1456,
		ExportStandardRate: 0.0980,
		ExportPeakRate:     0.2860,
		ExportOffPeakRate:  0.0524,
		GoPeakRate:         0.3453,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2802,
		IOGOffPeakRate:     0.0700,
	},
	"merseyside_wales": { // PES D / DNO 13
		RegionCode:         "D",
		ImportStandardRate: 0.2552,
		ImportPeakRate:     0.3573,
		ImportOffPeakRate:  0.1532,
		ExportStandardRate: 0.1054,
		ExportPeakRate:     0.3068,
		ExportOffPeakRate:  0.0512,
		GoPeakRate:         0.3575,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.3017,
		IOGOffPeakRate:     0.0700,
	},
	"west_midlands": { // PES E / DNO 14
		RegionCode:         "E",
		ImportStandardRate: 0.2328,
		ImportPeakRate:     0.3259,
		ImportOffPeakRate:  0.1397,
		ExportStandardRate: 0.0979,
		ExportPeakRate:     0.2781,
		ExportOffPeakRate:  0.0439,
		GoPeakRate:         0.3333,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2785,
		IOGOffPeakRate:     0.0700,
	},
	"north_east": { // PES F / DNO 15
		RegionCode:         "F",
		ImportStandardRate: 0.2319,
		ImportPeakRate:     0.3248,
		ImportOffPeakRate:  0.1392,
		ExportStandardRate: 0.0971,
		ExportPeakRate:     0.2769,
		ExportOffPeakRate:  0.0421,
		GoPeakRate:         0.3332,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2769,
		IOGOffPeakRate:     0.0700,
	},
	"north_west": { // PES G / DNO 16
		RegionCode:         "G",
		ImportStandardRate: 0.2407,
		ImportPeakRate:     0.3370,
		ImportOffPeakRate:  0.1445,
		ExportStandardRate: 0.1033,
		ExportPeakRate:     0.2925,
		ExportOffPeakRate:  0.0466,
		GoPeakRate:         0.3425,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2957,
		IOGOffPeakRate:     0.0700,
	},
	"southern": { // PES H / DNO 20
		RegionCode:         "H",
		ImportStandardRate: 0.2434,
		ImportPeakRate:     0.3408,
		ImportOffPeakRate:  0.1461,
		ExportStandardRate: 0.1008,
		ExportPeakRate:     0.2900,
		ExportOffPeakRate:  0.0474,
		GoPeakRate:         0.3481,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2879,
		IOGOffPeakRate:     0.0700,
	},
	"south_east": { // PES J / DNO 19
		RegionCode:         "J",
		ImportStandardRate: 0.2458,
		ImportPeakRate:     0.3441,
		ImportOffPeakRate:  0.1475,
		ExportStandardRate: 0.1024,
		ExportPeakRate:     0.2979,
		ExportOffPeakRate:  0.0505,
		GoPeakRate:         0.3490,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2937,
		IOGOffPeakRate:     0.0700,
	},
	"south_wales": { // PES K / DNO 21
		RegionCode:         "K",
		ImportStandardRate: 0.2426,
		ImportPeakRate:     0.3397,
		ImportOffPeakRate:  0.1456,
		ExportStandardRate: 0.1021,
		ExportPeakRate:     0.2935,
		ExportOffPeakRate:  0.0469,
		GoPeakRate:         0.3451,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2919,
		IOGOffPeakRate:     0.0700,
	},
	"south_west": { // PES L / DNO 22
		RegionCode:         "L",
		ImportStandardRate: 0.2432,
		ImportPeakRate:     0.3405,
		ImportOffPeakRate:  0.1460,
		ExportStandardRate: 0.1020,
		ExportPeakRate:     0.2922,
		ExportOffPeakRate:  0.0455,
		GoPeakRate:         0.3470,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2917,
		IOGOffPeakRate:     0.0700,
	},
	"yorkshire": { // PES M / DNO 23
		RegionCode:         "M",
		ImportStandardRate: 0.2323,
		ImportPeakRate:     0.3253,
		ImportOffPeakRate:  0.1395,
		ExportStandardRate: 0.0969,
		ExportPeakRate:     0.2717,
		ExportOffPeakRate:  0.0419,
		GoPeakRate:         0.3306,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2748,
		IOGOffPeakRate:     0.0700,
	},
	"southern_scotland": { // PES N / DNO 18
		RegionCode:         "N",
		ImportStandardRate: 0.2377,
		ImportPeakRate:     0.3328,
		ImportOffPeakRate:  0.1427,
		ExportStandardRate: 0.0955,
		ExportPeakRate:     0.2719,
		ExportOffPeakRate:  0.0442,
		GoPeakRate:         0.3345,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2722,
		IOGOffPeakRate:     0.0700,
	},
	"northern_scotland": { // PES P / DNO 17
		RegionCode:         "P",
		ImportStandardRate: 0.2437,
		ImportPeakRate:     0.3412,
		ImportOffPeakRate:  0.1463,
		ExportStandardRate: 0.0998,
		ExportPeakRate:     0.2873,
		ExportOffPeakRate:  0.0473,
		GoPeakRate:         0.3445,
		GoOffPeakRate:      0.0940,
		IOGPeakRate:        0.2868,
		IOGOffPeakRate:     0.0700,
	},
}

var octopusLocationChoices = []types.UtilityOptionChoice{
	{Value: "london", Name: "London (PES C / DNO 12)"},
	{Value: "eastern", Name: "Eastern England (PES A / DNO 10)"},
	{Value: "east_midlands", Name: "East Midlands (PES B / DNO 11)"},
	{Value: "merseyside_wales", Name: "Merseyside & North Wales (PES D / DNO 13)"},
	{Value: "west_midlands", Name: "West Midlands (PES E / DNO 14)"},
	{Value: "north_east", Name: "North East England (PES F / DNO 15)"},
	{Value: "north_west", Name: "North West England (PES G / DNO 16)"},
	{Value: "southern", Name: "Southern England (PES H / DNO 20)"},
	{Value: "south_east", Name: "South East England (PES J / DNO 19)"},
	{Value: "south_wales", Name: "South Wales (PES K / DNO 21)"},
	{Value: "south_west", Name: "South West England (PES L / DNO 22)"},
	{Value: "yorkshire", Name: "Yorkshire (PES M / DNO 23)"},
	{Value: "southern_scotland", Name: "Southern Scotland (PES N / DNO 18)"},
	{Value: "northern_scotland", Name: "Northern Scotland (PES P / DNO 17)"},
}

func normalizeOctopusLocation(location string) (string, octopusRateConfig, error) {
	loc := location
	if loc == "" {
		loc = "london"
	}
	cfg, ok := octopusRates[loc]
	if !ok {
		return "", octopusRateConfig{}, fmt.Errorf("unknown octopus location: %s", location)
	}
	return loc, cfg, nil
}

func octopusFluxFees(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	_, cfg, err := normalizeOctopusLocation(options.Location)
	if err != nil {
		return nil, err
	}

	var periods []types.UtilityFeesPeriod

	// 1. Off-Peak: 02:00 – 05:00
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Name:        "Off-Peak",
			LocationPtr: lonLocation,
			Hours:       []types.UtilityHourPeriod{{HourStart: 2, HourEnd: 5}},
		},
		DollarsPerKWH: cfg.ImportOffPeakRate,
		Description:   "Octopus Flux Off-Peak Rate",
	})
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Name:        "Off-Peak Export",
			LocationPtr: lonLocation,
			Hours:       []types.UtilityHourPeriod{{HourStart: 2, HourEnd: 5}},
		},
		DollarsPerKWH:            cfg.ExportOffPeakRate,
		SeparateGenerationCredit: true,
		Description:              "Octopus Flux Off-Peak Export Tariff",
	})

	// 2. Peak: 16:00 – 19:00
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Name:        "Peak",
			LocationPtr: lonLocation,
			Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
		},
		DollarsPerKWH: cfg.ImportPeakRate,
		Description:   "Octopus Flux Peak Rate",
	})
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Name:        "Peak Export",
			LocationPtr: lonLocation,
			Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
		},
		DollarsPerKWH:            cfg.ExportPeakRate,
		SeparateGenerationCredit: true,
		Description:              "Octopus Flux Peak Export Tariff",
	})

	// 3. Standard / Day: 05:00 – 16:00 and 19:00 – 02:00
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Name:        "Standard",
			LocationPtr: lonLocation,
			Hours: []types.UtilityHourPeriod{
				{HourStart: 5, HourEnd: 16},
				{HourStart: 19, HourEnd: 24},
				{HourStart: 0, HourEnd: 2},
			},
		},
		DollarsPerKWH: cfg.ImportStandardRate,
		Description:   "Octopus Flux Standard Rate",
	})
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod: types.TimePeriod{
			Name:        "Standard Export",
			LocationPtr: lonLocation,
			Hours: []types.UtilityHourPeriod{
				{HourStart: 5, HourEnd: 16},
				{HourStart: 19, HourEnd: 24},
				{HourStart: 0, HourEnd: 2},
			},
		},
		DollarsPerKWH:            cfg.ExportStandardRate,
		SeparateGenerationCredit: true,
		Description:              "Octopus Flux Standard Export Tariff",
	})

	return periods, nil
}

func octopusExportPeriods(options types.UtilityRateOptions) []types.UtilityFeesPeriod {
	exportRate := 0.1500 // Default: Outgoing Octopus Fixed 15p/kWh
	switch options.GenerationRate {
	case "outgoing_12p":
		exportRate = 0.1200
	case "outgoing_prime", "outgoing_agile", "none":
		exportRate = 0.0
	}

	if options.GenerationRate == "outgoing_prime" {
		// Prime Outgoing Octopus: 16p/kWh Peak (16:00 – 19:00), 9p/kWh Off-Peak (all other hours)
		return []types.UtilityFeesPeriod{
			{
				TimePeriod: types.TimePeriod{
					Name:        "Prime Export",
					LocationPtr: lonLocation,
					Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
				},
				DollarsPerKWH:            0.1600,
				SeparateGenerationCredit: true,
				Description:              "Prime Outgoing Octopus Peak Export Tariff",
			},
			{
				TimePeriod: types.TimePeriod{
					Name:        "Off-Peak Export",
					LocationPtr: lonLocation,
					Hours: []types.UtilityHourPeriod{
						{HourStart: 0, HourEnd: 16},
						{HourStart: 19, HourEnd: 24},
					},
				},
				DollarsPerKWH:            0.0900,
				SeparateGenerationCredit: true,
				Description:              "Prime Outgoing Octopus Off-Peak Export Tariff",
			},
		}
	}
	if exportRate > 0 {
		return []types.UtilityFeesPeriod{
			{
				TimePeriod: types.TimePeriod{
					LocationPtr: lonLocation,
				},
				DollarsPerKWH:            exportRate,
				SeparateGenerationCredit: true,
				Description:              "Outgoing Octopus Export Tariff",
			},
		}
	}
	return nil
}

func octopusGoFees(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	_, cfg, err := normalizeOctopusLocation(options.Location)
	if err != nil {
		return nil, err
	}

	periods := []types.UtilityFeesPeriod{
		// 1. Overnight Off-Peak: 00:30 – 05:30 (5 hours)
		{
			TimePeriod: types.TimePeriod{
				Name:        "Off-Peak",
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 0, MinuteStart: 30, HourEnd: 5, MinuteEnd: 30},
				},
			},
			DollarsPerKWH: cfg.GoOffPeakRate,
			Description:   "Octopus Go Off-Peak Rate",
		},
		// 2. Day Peak: 05:30 – 00:30 (19 hours)
		{
			TimePeriod: types.TimePeriod{
				Name:        "Peak",
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 0, HourEnd: 0, MinuteEnd: 30},
					{HourStart: 5, MinuteStart: 30, HourEnd: 24},
				},
			},
			DollarsPerKWH: cfg.GoPeakRate,
			Description:   "Octopus Go Day Rate",
		},
	}

	periods = append(periods, octopusExportPeriods(options)...)
	return periods, nil
}

func octopusIntelligentGoFees(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	_, cfg, err := normalizeOctopusLocation(options.Location)
	if err != nil {
		return nil, err
	}

	periods := []types.UtilityFeesPeriod{
		// 1. Overnight Off-Peak: 23:30 – 05:30 (6 hours)
		{
			TimePeriod: types.TimePeriod{
				Name:        "Off-Peak",
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 23, MinuteStart: 30, HourEnd: 24},
					{HourStart: 0, HourEnd: 5, MinuteEnd: 30},
				},
			},
			DollarsPerKWH: cfg.IOGOffPeakRate,
			Description:   "Intelligent Octopus Go Off-Peak Rate",
		},
		// 2. Day Peak: 05:30 – 23:30 (18 hours)
		{
			TimePeriod: types.TimePeriod{
				Name:        "Peak",
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 5, MinuteStart: 30, HourEnd: 23, MinuteEnd: 30},
				},
			},
			DollarsPerKWH: cfg.IOGPeakRate,
			Description:   "Intelligent Octopus Go Day Rate",
		},
	}

	periods = append(periods, octopusExportPeriods(options)...)
	return periods, nil
}

func octopusGoRateOptions() []types.UtilityRateOption {
	return []types.UtilityRateOption{
		{
			Field:       "location",
			Name:        "Distribution Network (DNO / PES Region)",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your regional electricity distribution network area.",
			Choices:     octopusLocationChoices,
			Default:     "london",
		},
		{
			Field:       "generationRate",
			Name:        "Export Tariff",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your export credit agreement (e.g. Outgoing Octopus).",
			Choices: []types.UtilityOptionChoice{
				{Value: "outgoing_15p", Name: "Outgoing Octopus Fixed (15p/kWh)"},
				{Value: "outgoing_12p", Name: "Outgoing Octopus Fixed (12p/kWh)"},
				{Value: "outgoing_prime", Name: "Prime Outgoing Octopus (16p Peak / 9p Off-Peak)"},
				{Value: "outgoing_agile", Name: "Agile Outgoing Octopus"},
				{Value: "none", Name: "No Export Credit"},
			},
			Default: "outgoing_15p",
		},
	}
}

// octopusUtilityInfo returns metadata for Octopus Energy (UK).
func octopusUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:          "octopus",
		Name:        "Octopus Energy (UK)",
		RatesNotice: "Intelligent Octopus Flux is not supported at this time.",
		Rates: []types.UtilityRateInfo{
			{
				ID:   "octopus_flux",
				Name: "Octopus Flux",
				Options: []types.UtilityRateOption{
					{
						Field:       "location",
						Name:        "Distribution Network (DNO / PES Region)",
						Type:        types.UtilityOptionTypeSelect,
						Description: "Select your regional electricity distribution network area.",
						Choices:     octopusLocationChoices,
						Default:     "london",
					},
				},
				GetFees: octopusFluxFees,
			},
			{
				ID:      "octopus_go",
				Name:    "Octopus Go",
				Options: octopusGoRateOptions(),
				GetFees: octopusGoFees,
			},
			{
				ID:      "octopus_intelligent_go",
				Name:    "Intelligent Octopus Go",
				Options: octopusGoRateOptions(),
				GetFees: octopusIntelligentGoFees,
			},
		},
	}
}

// baseOctopus implements the dynamic export pricing provider for Octopus Energy UK
// (specifically Agile Outgoing Octopus, product AGILE-OUTGOING-19-05-13).
type baseOctopus struct {
	apiURL          string
	client          *http.Client
	db              storage.Database
	mu              sync.Mutex
	cachedPrices    map[string]map[int64]types.Price // Key: location -> unix start seconds -> Price
	lastFutureFetch map[string]time.Time             // Key: location -> timestamp of last future API fetch
	nowFunc         func() time.Time
	sfGroup         singleflight.Group
}

func configuredOctopus(db storage.Database) *baseOctopus {
	c := &baseOctopus{
		apiURL:          defaultOctopusAPIURL,
		client:          common.HTTPClient(10 * time.Second),
		db:              db,
		cachedPrices:    make(map[string]map[int64]types.Price),
		lastFutureFetch: make(map[string]time.Time),
	}
	apiURL := lflag.String("octopus-api-url", defaultOctopusAPIURL, "URL for the Octopus Energy API")
	lflag.Do(func() {
		c.apiURL = *apiURL
	})
	return c
}

func (c *baseOctopus) now() time.Time {
	if c.nowFunc != nil {
		return c.nowFunc()
	}
	return time.Now()
}

// Region returns a UtilityPrices provider bound to a specific UK DNO / PES region.
func (c *baseOctopus) Region(location string) (*baseOctopusRegion, error) {
	loc, _, err := normalizeOctopusLocation(location)
	if err != nil {
		return nil, err
	}
	return &baseOctopusRegion{
		base:     c,
		location: loc,
	}, nil
}

// baseOctopusRegion binds baseOctopus to a specific UK distribution network region and implements UtilityPrices.
type baseOctopusRegion struct {
	base     *baseOctopus
	location string
}

func (r *baseOctopusRegion) settingsMatch(settings types.Settings) bool {
	loc, _, err := normalizeOctopusLocation(settings.UtilityRateOptions.Location)
	if err != nil {
		return false
	}
	return (settings.UtilityRate == "octopus_intelligent_go" || settings.UtilityRate == "octopus_go") &&
		settings.UtilityRateOptions.GenerationRate == "outgoing_agile" &&
		r.location == loc
}

func (r *baseOctopusRegion) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	return r.base.GetCurrentPrice(ctx, r.location)
}

func (r *baseOctopusRegion) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	return r.base.GetFuturePrices(ctx, r.location)
}

func (r *baseOctopusRegion) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	return r.base.GetConfirmedPrices(ctx, r.location, start, end)
}

type octopusUnitRateEntry struct {
	ValueExcVAT float64   `json:"value_exc_vat"`
	ValueIncVAT float64   `json:"value_inc_vat"`
	ValidFrom   time.Time `json:"valid_from"`
	ValidTo     time.Time `json:"valid_to"`
}

type octopusUnitRatesResponse struct {
	Count    int                    `json:"count"`
	Next     *string                `json:"next"`
	Previous *string                `json:"previous"`
	Results  []octopusUnitRateEntry `json:"results"`
}

func (c *baseOctopus) storeInMemory(loc string, prices []types.Price) {
	if len(prices) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedPrices == nil {
		c.cachedPrices = make(map[string]map[int64]types.Price)
	}
	if c.cachedPrices[loc] == nil {
		c.cachedPrices[loc] = make(map[int64]types.Price)
	}
	for _, p := range prices {
		p.TSStart = p.TSStart.In(lonLocation)
		p.TSEnd = p.TSEnd.In(lonLocation)
		c.cachedPrices[loc][p.TSStart.Unix()] = p
	}
}

// getRangeFromMemory returns all cached prices in [start, end) and reports whether every
// 30-minute interval in [checkStart, checkEnd) is present in memory.
func (c *baseOctopus) getRangeFromMemory(loc string, start, end, checkStart, checkEnd time.Time) ([]types.Price, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	locMap := c.cachedPrices[loc]
	if len(locMap) == 0 {
		return nil, false
	}

	allCovered := true
	slotStart := checkStart.In(lonLocation).Truncate(30 * time.Minute)
	slotEnd := checkEnd.In(lonLocation)
	if !slotStart.Before(slotEnd) {
		allCovered = false
	} else {
		for t := slotStart; t.Before(slotEnd); t = t.Add(30 * time.Minute) {
			if _, ok := locMap[t.Unix()]; !ok {
				allCovered = false
				break
			}
		}
	}

	var out []types.Price
	for _, p := range locMap {
		if !p.TSStart.Before(start) && p.TSStart.Before(end) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b types.Price) int {
		return a.TSStart.Compare(b.TSStart)
	})
	return out, allCovered
}

func (c *baseOctopus) loadFromDB(ctx context.Context, loc string, start, end time.Time) {
	if c.db == nil {
		return
	}
	dbUtilityID := "octopus_" + loc
	dbPrices, err := c.db.GetUtilityPrices(ctx, dbUtilityID, start, end)
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get octopus prices from database",
			slog.String("utilityID", dbUtilityID),
			slog.Any("error", err),
		)
		return
	}
	if len(dbPrices) == 0 {
		return
	}
	prices := make([]types.Price, 0, len(dbPrices))
	for _, ps := range dbPrices {
		p := ps.Price
		p.TSStart = p.TSStart.In(lonLocation)
		p.TSEnd = p.TSEnd.In(lonLocation)
		prices = append(prices, p)
	}
	c.storeInMemory(loc, prices)
}

// fetchOctopusRates queries the Octopus Energy standard-unit-rates endpoint for Agile Outgoing,
// caches the results in memory, and upserts them to the database.
func (c *baseOctopus) fetchOctopusRates(ctx context.Context, loc string, start, end time.Time) ([]types.Price, error) {
	loc, cfg, err := normalizeOctopusLocation(loc)
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimRight(c.apiURL, "/")
	if baseURL == "" {
		baseURL = defaultOctopusAPIURL
	}
	tariffCode := fmt.Sprintf("E-1R-%s-%s", octopusAgileOutgoingProduct, cfg.RegionCode)
	endpoint := fmt.Sprintf("%s/products/%s/electricity-tariffs/%s/standard-unit-rates/?period_from=%s&period_to=%s",
		baseURL,
		url.PathEscape(octopusAgileOutgoingProduct),
		url.PathEscape(tariffCode),
		url.QueryEscape(start.UTC().Format(time.RFC3339)),
		url.QueryEscape(end.UTC().Format(time.RFC3339)),
	)

	var allPrices []types.Price
	const maxPages = 10
	for page := 0; page < maxPages && endpoint != ""; page++ {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to request octopus unit rates: %w", err)
		}
		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("failed to read octopus unit rates response: %w", readErr)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("octopus api error status %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var parsed octopusUnitRatesResponse
		if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
			return nil, fmt.Errorf("failed to parse octopus unit rates response: %w", err)
		}

		for _, entry := range parsed.Results {
			if entry.ValidFrom.IsZero() || entry.ValidTo.IsZero() {
				continue
			}
			// Octopus returns unit rates in pence/kWh (p/kWh); divide by 100 to convert to £/kWh.
			creditGBPPerKWH := entry.ValueIncVAT / 100.0
			allPrices = append(allPrices, types.Price{
				Provider:                      "octopus",
				TSStart:                       entry.ValidFrom.In(lonLocation),
				TSEnd:                         entry.ValidTo.In(lonLocation),
				DollarsPerKWH:                 0,
				GenerationCreditDollarsPerKWH: creditGBPPerKWH,
				SeparateGenerationCredit:      true,
			})
		}

		if parsed.Next != nil && *parsed.Next != "" {
			endpoint = *parsed.Next
		} else {
			endpoint = ""
		}
	}

	if len(allPrices) == 0 {
		return nil, fmt.Errorf("no octopus unit rates returned for %s between %s and %s",
			loc, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}

	slices.SortFunc(allPrices, func(a, b types.Price) int {
		return a.TSStart.Compare(b.TSStart)
	})

	c.storeInMemory(loc, allPrices)

	if c.db != nil {
		nowTime := c.now()
		toUpsert := make([]types.PriceState, 0, len(allPrices))
		for _, p := range allPrices {
			toUpsert = append(toUpsert, types.PriceState{
				Price:     p,
				Confirmed: true,
				TSUpdated: nowTime,
			})
		}
		dbUtilityID := "octopus_" + loc
		if err := c.db.UpsertUtilityPrices(ctx, dbUtilityID, toUpsert, 0); err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to upsert octopus prices to database",
				slog.String("utilityID", dbUtilityID),
				slog.Any("error", err),
			)
		}
	}

	return allPrices, nil
}

// GetCurrentPrice returns the half-hourly Agile Outgoing rate for the current 30-minute settlement period.
func (c *baseOctopus) GetCurrentPrice(ctx context.Context, location string) (types.Price, error) {
	nowLon := c.now().In(lonLocation)
	slotStart := nowLon.Truncate(30 * time.Minute)
	prices, err := c.getPrices(ctx, location, slotStart, slotStart.Add(30*time.Minute))
	if err != nil {
		return types.Price{}, fmt.Errorf("failed to get current octopus price for %s: %w", location, err)
	}
	if len(prices) == 0 {
		return types.Price{}, fmt.Errorf("no current octopus price found for %s at %s", location, nowLon.Format(time.RFC3339))
	}
	return prices[0], nil
}

// GetFuturePrices returns the published half-hourly Agile Outgoing rates from now onward.
//
// Octopus Agile Publication Timeline:
// Octopus publishes next-day half-hourly rates (covering 23:00 UK local time today through 23:00 UK local time tomorrow)
// around 16:00 UK local time each day.
//   - Prior to 16:00 UK time: Published rates are available up to 23:00 today.
//   - At or after 16:00 UK time: Published rates are available up to 23:00 tomorrow.
//
// GetFuturePrices returns only the actual published rates from Octopus without fabricating or predicting
// missing future intervals (Server.getFuturePrices estimates any un-published hours from historical data).
func (c *baseOctopus) GetFuturePrices(ctx context.Context, location string) ([]types.Price, error) {
	nowLon := c.now().In(lonLocation)
	firstFutureSlot := nowLon.Truncate(30 * time.Minute).Add(30 * time.Minute)
	tomorrowEnd := truncateDay(nowLon).AddDate(0, 0, 2)
	prices, err := c.getPrices(ctx, location, firstFutureSlot, tomorrowEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to get future octopus prices for %s: %w", location, err)
	}
	if len(prices) == 0 {
		return nil, fmt.Errorf("no future octopus prices found for %s", location)
	}
	return prices, nil
}

// GetConfirmedPrices returns confirmed half-hourly Agile Outgoing rates for the specified time range,
// checking in-memory cache and Firestore before querying the Octopus Energy API.
func (c *baseOctopus) GetConfirmedPrices(ctx context.Context, location string, start, end time.Time) ([]types.Price, error) {
	return c.getPrices(ctx, location, start, end)
}

// getPrices retrieves half-hourly Agile Outgoing rates for [start, end), checking in-memory cache
// and Firestore before querying the Octopus Energy API. Because Agile Outgoing rates are day-ahead
// settled and final as soon as published, GetCurrentPrice, GetFuturePrices, and GetConfirmedPrices
// all share this unified lookup path.
func (c *baseOctopus) getPrices(ctx context.Context, location string, start, end time.Time) ([]types.Price, error) {
	loc, _, err := normalizeOctopusLocation(location)
	if err != nil {
		return nil, err
	}
	if !start.Before(end) {
		return nil, nil
	}

	now := c.now()
	nowLon := now.In(lonLocation)
	today := truncateDay(nowLon)
	todayPublishEnd := time.Date(today.Year(), today.Month(), today.Day(), 23, 0, 0, 0, lonLocation)
	tomorrow := today.AddDate(0, 0, 1)
	tomorrowPublishEnd := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 23, 0, 0, 0, lonLocation)
	tomorrowEnd := today.AddDate(0, 0, 2)

	expectedEnd := todayPublishEnd
	if nowLon.Hour() >= 16 {
		expectedEnd = tomorrowPublishEnd
	}

	startLon := start.In(lonLocation).Truncate(30 * time.Minute)
	endLon := end.In(lonLocation)
	if endLon.Truncate(30 * time.Minute).Before(endLon) {
		endLon = endLon.Truncate(30 * time.Minute).Add(30 * time.Minute)
	}

	checkEnd := endLon
	if checkEnd.After(expectedEnd) {
		checkEnd = expectedEnd
	}

	// If the requested range starts at or after the latest published horizon (expectedEnd),
	// return any cached slots without querying the DB or API for unpublished future dates.
	if !startLon.Before(checkEnd) {
		prices, _ := c.getRangeFromMemory(loc, startLon, endLon, startLon, startLon)
		return copyPrices(prices), nil
	}

	if prices, allCovered := c.getRangeFromMemory(loc, startLon, endLon, startLon, checkEnd); allCovered && len(prices) > 0 {
		return copyPrices(prices), nil
	}

	fetchStart := startLon
	if fetchStart.After(today) && fetchStart.Before(tomorrowEnd) {
		fetchStart = today
	}
	fetchEnd := endLon
	if fetchEnd.After(today) && fetchEnd.Before(tomorrowEnd) {
		fetchEnd = tomorrowEnd
	}

	sfKey := fmt.Sprintf("prices_%s_%d_%d", loc, fetchStart.Unix(), fetchEnd.Unix())
	_, err, _ = c.sfGroup.Do(sfKey, func() (any, error) {
		prices, allCovered := c.getRangeFromMemory(loc, startLon, endLon, startLon, checkEnd)
		if allCovered && len(prices) > 0 {
			return nil, nil
		}

		// 1. Check database first which retrieves a wider range than we want
		c.loadFromDB(ctx, loc, fetchStart, fetchEnd)
		// then filter to see if we have all of the range covered
		prices, allCovered = c.getRangeFromMemory(loc, startLon, endLon, startLon, checkEnd)
		if allCovered && len(prices) > 0 {
			return nil, nil
		}

		c.mu.Lock()
		lastFetch := c.lastFutureFetch[loc]
		c.mu.Unlock()

		// If checking into the post-23:00 window after 16:00 and we recently queried the API,
		// avoid hammering the API while tomorrow's publication is delayed if we already have prices.
		if checkEnd.After(todayPublishEnd) && !lastFetch.IsZero() && now.Sub(lastFetch) < 15*time.Minute && len(prices) > 0 {
			return nil, nil
		}

		// 2. Fetch from Octopus API
		if _, err := c.fetchOctopusRates(ctx, loc, fetchStart, fetchEnd); err != nil {
			if len(prices) > 0 {
				log.Ctx(ctx).WarnContext(ctx, "failed to refresh octopus prices from api, using cached prices",
					slog.String("location", loc),
					slog.Any("error", err),
				)
				return nil, nil
			}
			return nil, err
		}
		// store when we last fetched only if we expected there to be data
		if nowLon.Hour() >= 16 && !fetchEnd.Before(tomorrowPublishEnd) {
			c.mu.Lock()
			if c.lastFutureFetch == nil {
				c.lastFutureFetch = make(map[string]time.Time)
			}
			c.lastFutureFetch[loc] = c.now()
			c.mu.Unlock()
		}

		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	prices, _ := c.getRangeFromMemory(loc, startLon, endLon, startLon, checkEnd)
	return copyPrices(prices), nil
}
