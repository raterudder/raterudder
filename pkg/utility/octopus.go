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

// Tariff Audit Metadata
// - Provider(s): octopus (Octopus Energy (UK))
// - Official Sources:
//   - https://api.octopus.energy/v1/products/?brand=OCTOPUS_ENERGY&is_business=false
//   - https://api.octopus.energy/v1/products/FLUX-IMPORT-23-02-14/ & https://api.octopus.energy/v1/products/FLUX-EXPORT-23-02-14/ (Octopus Flux Import & Export, revised eff. 2026-10-19)
//   - https://api.octopus.energy/v1/products/INTELLI-FLUX-IMPORT-23-07-14/ & https://api.octopus.energy/v1/graphql/ (Intelligent Octopus Flux Import & INTELLI-FLUX-EXPORT-23-07-14 1:1 parity)
//   - https://api.octopus.energy/v1/products/GO-VAR-22-10-14/ & https://api.octopus.energy/v1/products/GO-FIX-12M-26-10-30/ (Octopus Go Flexible & 12M Fixed)
//   - https://api.octopus.energy/v1/products/IOG-SMB-VAR-26-11-04/, https://api.octopus.energy/v1/products/IOG-SMB-VAR-26-10-29/, https://api.octopus.energy/v1/products/IOG-SMB-FIX-12M-26-11-11/, https://api.octopus.energy/v1/products/INTELLI-VAR-24-10-29/, & https://api.octopus.energy/v1/products/INTELLI-VAR-22-10-14/ (Intelligent Octopus Go Flexible, Max, 12M Fixed, Legacy Oct 2024, & pre-2026-11-04 historical)
//   - https://api.octopus.energy/v1/products/COSY-22-12-08/ & https://api.octopus.energy/v1/products/COSY-FIX-12M-26-10-30/ (Cosy Octopus Flexible & 12M Fixed)
//   - https://api.octopus.energy/v1/products/OUTGOING-VAR-24-10-26/, https://api.octopus.energy/v1/products/OUTGOING-PRIME-FIX-12M-26-06-23/, & https://api.octopus.energy/v1/products/AGILE-OUTGOING-19-05-13/ (Outgoing Octopus 12p/15p, Prime Outgoing, & live Agile Outgoing)
// - Effective Window: 2026-10-01 through until superseded (Octopus Flux revised 2026-10-19; Outgoing Octopus 12p effective 2026-03-01; IOG flexible revised 2026-11-04 & variants effective Oct/Nov 2026)
// - Revision Cadence: Quarterly (Jan 1, Apr 1, Jul 1, Oct 1 Ofgem Price Cap) & Half-Hourly (Live Agile Outgoing REST API)
// - Next Audit Due: 2027-01-05 (Check Q1 2027 Ofgem price cap revisions effective Jan 1, 2027 across GSP regions _A through _P)
// - Last Verified: 2026-11-21
// - Modeling Notes: Unit rates are stored in GBP/kWh (£/kWh in DollarsPerKWH fields) inclusive of 5% UK domestic VAT; daily standing charges (p/day) are excluded from volumetric periods; pre-2026-10-19 Octopus Flux and pre-2026-11-04 Intelligent Octopus Go (INTELLI-VAR-22-10-14) rates are preserved for historical continuity; Intelligent Octopus Flux models 1:1 import and SeparateGenerationCredit export parity (16:00-19:00 Peak, 00:00-16:00 and 19:00-24:00 Standard); Outgoing export defaults to 12p/kWh (OUTGOING-VAR-24-10-26) while retaining 15p/kWh fixed, Prime Outgoing (16p/9p), live Agile Outgoing, and No Export options.
//
// Octopus Energy REST API Reference:
// Base URL: https://api.octopus.energy/v1/products/
// Tariff Code Format: E-1R-{PRODUCT_CODE}-{REGION_LETTER} (e.g. E-1R-AGILE-OUTGOING-19-05-13-C for London)

const (
	defaultOctopusAPIURL        = "https://api.octopus.energy/v1"
	octopusAgileOutgoingProduct = "AGILE-OUTGOING-19-05-13"

	// National Outgoing Octopus export credit rates (£/kWh).
	octopusOutgoing12pDollarsPerKWH        = 0.1200 // OUTGOING-VAR-24-10-26 (effective 2026-03-01)
	octopusOutgoing15pDollarsPerKWH        = 0.1500 // Legacy / fixed 15p Outgoing Octopus
	octopusPrimeExportPeakDollarsPerKWH    = 0.1600 // OUTGOING-PRIME-FIX-12M-26-06-23 Peak (16:00-19:00)
	octopusPrimeExportOffPeakDollarsPerKWH = 0.0900 // OUTGOING-PRIME-FIX-12M-26-06-23 Off-Peak

	// National overnight off-peak import unit rates (£/kWh inc. 5% VAT).
	octopusGoFixedOffPeakDollarsPerKWH     = 0.0940 // GO-FIX-12M-26-10-30 (00:30-05:30)
	octopusIOGFlexibleOffPeakDollarsPerKWH = 0.0990 // IOG-SMB-VAR-26-11-04 (23:30-05:30)
	octopusIOGMaxOffPeakDollarsPerKWH      = 0.0650 // IOG-SMB-VAR-26-10-29 (23:30-05:30)
	octopusIOGFixedOffPeakDollarsPerKWH    = 0.0790 // IOG-SMB-FIX-12M-26-11-11 (23:30-05:30)
	octopusIOGLegacyOffPeakDollarsPerKWH   = 0.0660 // INTELLI-VAR-24-10-29 (23:30-05:30)
)

type octopusRateConfig struct {
	RegionCode string

	// Pre-2026-10-19 Octopus Flux rates preserved for historical continuity.
	ImportPeakRate     float64
	ImportOffPeakRate  float64
	ImportStandardRate float64

	ExportPeakRate     float64
	ExportOffPeakRate  float64
	ExportStandardRate float64

	// Q4 2026 Octopus Flux rates (FLUX-IMPORT-23-02-14 & FLUX-EXPORT-23-02-14, eff. 2026-10-19).
	FluxImportStandardDollarsPerKWH float64
	FluxImportPeakDollarsPerKWH     float64
	FluxImportOffPeakDollarsPerKWH  float64

	FluxExportStandardDollarsPerKWH float64
	FluxExportPeakDollarsPerKWH     float64
	FluxExportOffPeakDollarsPerKWH  float64

	// Q4 2026 Intelligent Octopus Flux 1:1 import/export parity rates
	// (INTELLI-FLUX-IMPORT-23-07-14 & INTELLI-FLUX-EXPORT-23-07-14).
	IntelliFluxStandardDollarsPerKWH float64
	IntelliFluxPeakDollarsPerKWH     float64

	// Octopus Go Flexible (GO-VAR-22-10-14) & 12M Fixed (GO-FIX-12M-26-10-30).
	GoPeakRate               float64
	GoOffPeakRate            float64
	GoFixedPeakDollarsPerKWH float64

	// Pre-2026-11-04 Intelligent Octopus Go rates (INTELLI-VAR-22-10-14) preserved for historical continuity.
	IOGPeakRate    float64
	IOGOffPeakRate float64

	// Intelligent Octopus Go Q4 2026 regional day/peak rates (£/kWh inc. 5% VAT).
	IOGFlexiblePeakDollarsPerKWH float64 // IOG-SMB-VAR-26-11-04
	IOGMaxPeakDollarsPerKWH      float64 // IOG-SMB-VAR-26-10-29
	IOGFixedPeakDollarsPerKWH    float64 // IOG-SMB-FIX-12M-26-11-11
	IOGLegacyPeakDollarsPerKWH   float64 // INTELLI-VAR-24-10-29

	// Cosy Octopus Flexible (COSY-22-12-08) & 12M Fixed (COSY-FIX-12M-26-10-30).
	CosyPeakRate     float64
	CosyOffPeakRate  float64
	CosyStandardRate float64

	CosyFixedPeakRate     float64
	CosyFixedOffPeakRate  float64
	CosyFixedStandardRate float64
}

// octopusRates stores unit rates in base currency (£/kWh inc. 5% VAT, e.g. 0.2450 = 24.5p/kWh)
// indexed by the 14 UK distribution network (DNO / PES / GSP) regions A through P.
var octopusRates = map[string]octopusRateConfig{
	"eastern": { // PES A / DNO 10
		RegionCode:                       "A",
		ImportStandardRate:               0.2429,
		ImportPeakRate:                   0.3401,
		ImportOffPeakRate:                0.1458,
		ExportStandardRate:               0.1011,
		ExportPeakRate:                   0.2932,
		ExportOffPeakRate:                0.0499,
		FluxImportStandardDollarsPerKWH:  0.267930,
		FluxImportPeakDollarsPerKWH:      0.374090,
		FluxImportOffPeakDollarsPerKWH:   0.161769,
		FluxExportStandardDollarsPerKWH:  0.111216,
		FluxExportPeakDollarsPerKWH:      0.303317,
		FluxExportOffPeakDollarsPerKWH:   0.047216,
		IntelliFluxStandardDollarsPerKWH: 0.242653,
		IntelliFluxPeakDollarsPerKWH:     0.303317,
		GoPeakRate:                       0.3471,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.383917,
		IOGPeakRate:                      0.2896,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.344638,
		IOGMaxPeakDollarsPerKWH:          0.386245,
		IOGFixedPeakDollarsPerKWH:        0.405800,
		IOGLegacyPeakDollarsPerKWH:       0.362940,
		CosyStandardRate:                 0.2882,
		CosyPeakRate:                     0.4322,
		CosyOffPeakRate:                  0.1413,
		CosyFixedStandardRate:            0.3276,
		CosyFixedPeakRate:                0.4913,
		CosyFixedOffPeakRate:             0.1607,
	},
	"east_midlands": { // PES B / DNO 11
		RegionCode:                       "B",
		ImportStandardRate:               0.2306,
		ImportPeakRate:                   0.3229,
		ImportOffPeakRate:                0.1384,
		ExportStandardRate:               0.0976,
		ExportPeakRate:                   0.2770,
		ExportOffPeakRate:                0.0434,
		FluxImportStandardDollarsPerKWH:  0.255265,
		FluxImportPeakDollarsPerKWH:      0.356407,
		FluxImportOffPeakDollarsPerKWH:   0.154122,
		FluxExportStandardDollarsPerKWH:  0.105959,
		FluxExportPeakDollarsPerKWH:      0.288979,
		FluxExportOffPeakDollarsPerKWH:   0.044984,
		IntelliFluxStandardDollarsPerKWH: 0.231183,
		IntelliFluxPeakDollarsPerKWH:     0.288979,
		GoPeakRate:                       0.3303,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.362868,
		IOGPeakRate:                      0.2777,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.325278,
		IOGMaxPeakDollarsPerKWH:          0.364417,
		IOGFixedPeakDollarsPerKWH:        0.381785,
		IOGLegacyPeakDollarsPerKWH:       0.346640,
		CosyStandardRate:                 0.2745,
		CosyPeakRate:                     0.4118,
		CosyOffPeakRate:                  0.1346,
		CosyFixedStandardRate:            0.3130,
		CosyFixedPeakRate:                0.4694,
		CosyFixedOffPeakRate:             0.1536,
	},
	"london": { // PES C / DNO 12
		RegionCode:                       "C",
		ImportStandardRate:               0.2425,
		ImportPeakRate:                   0.3396,
		ImportOffPeakRate:                0.1456,
		ExportStandardRate:               0.0980,
		ExportPeakRate:                   0.2860,
		ExportOffPeakRate:                0.0524,
		FluxImportStandardDollarsPerKWH:  0.267752,
		FluxImportPeakDollarsPerKWH:      0.373843,
		FluxImportOffPeakDollarsPerKWH:   0.161662,
		FluxExportStandardDollarsPerKWH:  0.111143,
		FluxExportPeakDollarsPerKWH:      0.303116,
		FluxExportOffPeakDollarsPerKWH:   0.047185,
		IntelliFluxStandardDollarsPerKWH: 0.242493,
		IntelliFluxPeakDollarsPerKWH:     0.303116,
		GoPeakRate:                       0.3453,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.383123,
		IOGPeakRate:                      0.2802,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.341553,
		IOGMaxPeakDollarsPerKWH:          0.382729,
		IOGFixedPeakDollarsPerKWH:        0.404354,
		IOGLegacyPeakDollarsPerKWH:       0.361200,
		CosyStandardRate:                 0.2880,
		CosyPeakRate:                     0.4319,
		CosyOffPeakRate:                  0.1412,
		CosyFixedStandardRate:            0.3495,
		CosyFixedPeakRate:                0.5242,
		CosyFixedOffPeakRate:             0.1715,
	},
	"merseyside_wales": { // PES D / DNO 13
		RegionCode:                       "D",
		ImportStandardRate:               0.2552,
		ImportPeakRate:                   0.3573,
		ImportOffPeakRate:                0.1532,
		ExportStandardRate:               0.1054,
		ExportPeakRate:                   0.3068,
		ExportOffPeakRate:                0.0512,
		FluxImportStandardDollarsPerKWH:  0.280502,
		FluxImportPeakDollarsPerKWH:      0.391644,
		FluxImportOffPeakDollarsPerKWH:   0.169360,
		FluxExportStandardDollarsPerKWH:  0.116435,
		FluxExportPeakDollarsPerKWH:      0.317549,
		FluxExportOffPeakDollarsPerKWH:   0.049432,
		IntelliFluxStandardDollarsPerKWH: 0.254040,
		IntelliFluxPeakDollarsPerKWH:     0.317549,
		GoPeakRate:                       0.3575,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.403378,
		IOGPeakRate:                      0.3017,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.363146,
		IOGMaxPeakDollarsPerKWH:          0.407225,
		IOGFixedPeakDollarsPerKWH:        0.427067,
		IOGLegacyPeakDollarsPerKWH:       0.373086,
		CosyStandardRate:                 0.3017,
		CosyPeakRate:                     0.4525,
		CosyOffPeakRate:                  0.1479,
		CosyFixedStandardRate:            0.3432,
		CosyFixedPeakRate:                0.5149,
		CosyFixedOffPeakRate:             0.1684,
	},
	"west_midlands": { // PES E / DNO 14
		RegionCode:                       "E",
		ImportStandardRate:               0.2328,
		ImportPeakRate:                   0.3259,
		ImportOffPeakRate:                0.1397,
		ExportStandardRate:               0.0979,
		ExportPeakRate:                   0.2781,
		ExportOffPeakRate:                0.0439,
		FluxImportStandardDollarsPerKWH:  0.257761,
		FluxImportPeakDollarsPerKWH:      0.359893,
		FluxImportOffPeakDollarsPerKWH:   0.155630,
		FluxExportStandardDollarsPerKWH:  0.106995,
		FluxExportPeakDollarsPerKWH:      0.291805,
		FluxExportOffPeakDollarsPerKWH:   0.045424,
		IntelliFluxStandardDollarsPerKWH: 0.233444,
		IntelliFluxPeakDollarsPerKWH:     0.291805,
		GoPeakRate:                       0.3333,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.367501,
		IOGPeakRate:                      0.2785,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.330171,
		IOGMaxPeakDollarsPerKWH:          0.369753,
		IOGFixedPeakDollarsPerKWH:        0.386704,
		IOGLegacyPeakDollarsPerKWH:       0.349561,
		CosyStandardRate:                 0.2772,
		CosyPeakRate:                     0.4158,
		CosyOffPeakRate:                  0.1359,
		CosyFixedStandardRate:            0.3167,
		CosyFixedPeakRate:                0.4751,
		CosyFixedOffPeakRate:             0.1554,
	},
	"north_east": { // PES F / DNO 15
		RegionCode:                       "F",
		ImportStandardRate:               0.2319,
		ImportPeakRate:                   0.3248,
		ImportOffPeakRate:                0.1392,
		ExportStandardRate:               0.0971,
		ExportPeakRate:                   0.2769,
		ExportOffPeakRate:                0.0421,
		FluxImportStandardDollarsPerKWH:  0.256271,
		FluxImportPeakDollarsPerKWH:      0.357813,
		FluxImportOffPeakDollarsPerKWH:   0.154730,
		FluxExportStandardDollarsPerKWH:  0.106377,
		FluxExportPeakDollarsPerKWH:      0.290119,
		FluxExportOffPeakDollarsPerKWH:   0.045162,
		IntelliFluxStandardDollarsPerKWH: 0.232095,
		IntelliFluxPeakDollarsPerKWH:     0.290119,
		GoPeakRate:                       0.3332,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.366442,
		IOGPeakRate:                      0.2769,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.328789,
		IOGMaxPeakDollarsPerKWH:          0.368540,
		IOGFixedPeakDollarsPerKWH:        0.386270,
		IOGLegacyPeakDollarsPerKWH:       0.349472,
		CosyStandardRate:                 0.2756,
		CosyPeakRate:                     0.4134,
		CosyOffPeakRate:                  0.1352,
		CosyFixedStandardRate:            0.3128,
		CosyFixedPeakRate:                0.4692,
		CosyFixedOffPeakRate:             0.1535,
	},
	"north_west": { // PES G / DNO 16
		RegionCode:                       "G",
		ImportStandardRate:               0.2407,
		ImportPeakRate:                   0.3370,
		ImportOffPeakRate:                0.1445,
		ExportStandardRate:               0.1033,
		ExportPeakRate:                   0.2925,
		ExportOffPeakRate:                0.0466,
		FluxImportStandardDollarsPerKWH:  0.266681,
		FluxImportPeakDollarsPerKWH:      0.372347,
		FluxImportOffPeakDollarsPerKWH:   0.161015,
		FluxExportStandardDollarsPerKWH:  0.110698,
		FluxExportPeakDollarsPerKWH:      0.301903,
		FluxExportOffPeakDollarsPerKWH:   0.046996,
		IntelliFluxStandardDollarsPerKWH: 0.241523,
		IntelliFluxPeakDollarsPerKWH:     0.301903,
		GoPeakRate:                       0.3425,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.384711,
		IOGPeakRate:                      0.2957,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.341340,
		IOGMaxPeakDollarsPerKWH:          0.382607,
		IOGFixedPeakDollarsPerKWH:        0.406090,
		IOGLegacyPeakDollarsPerKWH:       0.358518,
		CosyStandardRate:                 0.2868,
		CosyPeakRate:                     0.4302,
		CosyOffPeakRate:                  0.1407,
		CosyFixedStandardRate:            0.3208,
		CosyFixedPeakRate:                0.4812,
		CosyFixedOffPeakRate:             0.1574,
	},
	"southern": { // PES H / DNO 20
		RegionCode:                       "H",
		ImportStandardRate:               0.2434,
		ImportPeakRate:                   0.3408,
		ImportOffPeakRate:                0.1461,
		ExportStandardRate:               0.1008,
		ExportPeakRate:                   0.2900,
		ExportOffPeakRate:                0.0474,
		FluxImportStandardDollarsPerKWH:  0.266452,
		FluxImportPeakDollarsPerKWH:      0.372027,
		FluxImportOffPeakDollarsPerKWH:   0.160877,
		FluxExportStandardDollarsPerKWH:  0.110603,
		FluxExportPeakDollarsPerKWH:      0.301643,
		FluxExportOffPeakDollarsPerKWH:   0.046956,
		IntelliFluxStandardDollarsPerKWH: 0.241315,
		IntelliFluxPeakDollarsPerKWH:     0.301643,
		GoPeakRate:                       0.3481,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.375445,
		IOGPeakRate:                      0.2879,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.336022,
		IOGMaxPeakDollarsPerKWH:          0.376786,
		IOGFixedPeakDollarsPerKWH:        0.396252,
		IOGLegacyPeakDollarsPerKWH:       0.363918,
		CosyStandardRate:                 0.2866,
		CosyPeakRate:                     0.4298,
		CosyOffPeakRate:                  0.1405,
		CosyFixedStandardRate:            0.3208,
		CosyFixedPeakRate:                0.4812,
		CosyFixedOffPeakRate:             0.1574,
	},
	"south_east": { // PES J / DNO 19
		RegionCode:                       "J",
		ImportStandardRate:               0.2458,
		ImportPeakRate:                   0.3441,
		ImportOffPeakRate:                0.1475,
		ExportStandardRate:               0.1024,
		ExportPeakRate:                   0.2979,
		ExportOffPeakRate:                0.0505,
		FluxImportStandardDollarsPerKWH:  0.270612,
		FluxImportPeakDollarsPerKWH:      0.377835,
		FluxImportOffPeakDollarsPerKWH:   0.163388,
		FluxExportStandardDollarsPerKWH:  0.112329,
		FluxExportPeakDollarsPerKWH:      0.306353,
		FluxExportOffPeakDollarsPerKWH:   0.047689,
		IntelliFluxStandardDollarsPerKWH: 0.245082,
		IntelliFluxPeakDollarsPerKWH:     0.306353,
		GoPeakRate:                       0.3490,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.386300,
		IOGPeakRate:                      0.2937,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.347403,
		IOGMaxPeakDollarsPerKWH:          0.389520,
		IOGFixedPeakDollarsPerKWH:        0.407537,
		IOGLegacyPeakDollarsPerKWH:       0.364808,
		CosyStandardRate:                 0.2910,
		CosyPeakRate:                     0.4366,
		CosyOffPeakRate:                  0.1427,
		CosyFixedStandardRate:            0.3111,
		CosyFixedPeakRate:                0.4666,
		CosyFixedOffPeakRate:             0.1527,
	},
	"south_wales": { // PES K / DNO 21
		RegionCode:                       "K",
		ImportStandardRate:               0.2426,
		ImportPeakRate:                   0.3397,
		ImportOffPeakRate:                0.1456,
		ExportStandardRate:               0.1021,
		ExportPeakRate:                   0.2935,
		ExportOffPeakRate:                0.0469,
		FluxImportStandardDollarsPerKWH:  0.267334,
		FluxImportPeakDollarsPerKWH:      0.373258,
		FluxImportOffPeakDollarsPerKWH:   0.161409,
		FluxExportStandardDollarsPerKWH:  0.110969,
		FluxExportPeakDollarsPerKWH:      0.302642,
		FluxExportOffPeakDollarsPerKWH:   0.047111,
		IntelliFluxStandardDollarsPerKWH: 0.242114,
		IntelliFluxPeakDollarsPerKWH:     0.302642,
		GoPeakRate:                       0.3451,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.382726,
		IOGPeakRate:                      0.2919,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.341234,
		IOGMaxPeakDollarsPerKWH:          0.382729,
		IOGFixedPeakDollarsPerKWH:        0.403920,
		IOGLegacyPeakDollarsPerKWH:       0.360965,
		CosyStandardRate:                 0.2875,
		CosyPeakRate:                     0.4313,
		CosyOffPeakRate:                  0.1410,
		CosyFixedStandardRate:            0.3178,
		CosyFixedPeakRate:                0.4767,
		CosyFixedOffPeakRate:             0.1560,
	},
	"south_west": { // PES L / DNO 22
		RegionCode:                       "L",
		ImportStandardRate:               0.2432,
		ImportPeakRate:                   0.3405,
		ImportOffPeakRate:                0.1460,
		ExportStandardRate:               0.1020,
		ExportPeakRate:                   0.2922,
		ExportOffPeakRate:                0.0455,
		FluxImportStandardDollarsPerKWH:  0.267616,
		FluxImportPeakDollarsPerKWH:      0.373652,
		FluxImportOffPeakDollarsPerKWH:   0.161579,
		FluxExportStandardDollarsPerKWH:  0.111086,
		FluxExportPeakDollarsPerKWH:      0.302961,
		FluxExportOffPeakDollarsPerKWH:   0.047161,
		IntelliFluxStandardDollarsPerKWH: 0.242369,
		IntelliFluxPeakDollarsPerKWH:     0.302961,
		GoPeakRate:                       0.3470,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.382858,
		IOGPeakRate:                      0.2917,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.339957,
		IOGMaxPeakDollarsPerKWH:          0.381273,
		IOGFixedPeakDollarsPerKWH:        0.404064,
		IOGLegacyPeakDollarsPerKWH:       0.362828,
		CosyStandardRate:                 0.2878,
		CosyPeakRate:                     0.4317,
		CosyOffPeakRate:                  0.1411,
		CosyFixedStandardRate:            0.3155,
		CosyFixedPeakRate:                0.4733,
		CosyFixedOffPeakRate:             0.1548,
	},
	"yorkshire": { // PES M / DNO 23
		RegionCode:                       "M",
		ImportStandardRate:               0.2323,
		ImportPeakRate:                   0.3253,
		ImportOffPeakRate:                0.1395,
		ExportStandardRate:               0.0969,
		ExportPeakRate:                   0.2717,
		ExportOffPeakRate:                0.0419,
		FluxImportStandardDollarsPerKWH:  0.257757,
		FluxImportPeakDollarsPerKWH:      0.359888,
		FluxImportOffPeakDollarsPerKWH:   0.155627,
		FluxExportStandardDollarsPerKWH:  0.106994,
		FluxExportPeakDollarsPerKWH:      0.291801,
		FluxExportOffPeakDollarsPerKWH:   0.045424,
		IntelliFluxStandardDollarsPerKWH: 0.233441,
		IntelliFluxPeakDollarsPerKWH:     0.291801,
		GoPeakRate:                       0.3306,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.369355,
		IOGPeakRate:                      0.2748,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.333362,
		IOGMaxPeakDollarsPerKWH:          0.373270,
		IOGFixedPeakDollarsPerKWH:        0.389163,
		IOGLegacyPeakDollarsPerKWH:       0.346899,
		CosyStandardRate:                 0.2772,
		CosyPeakRate:                     0.4158,
		CosyOffPeakRate:                  0.1359,
		CosyFixedStandardRate:            0.3257,
		CosyFixedPeakRate:                0.4885,
		CosyFixedOffPeakRate:             0.1598,
	},
	"southern_scotland": { // PES N / DNO 18
		RegionCode:                       "N",
		ImportStandardRate:               0.2377,
		ImportPeakRate:                   0.3328,
		ImportOffPeakRate:                0.1427,
		ExportStandardRate:               0.0955,
		ExportPeakRate:                   0.2719,
		ExportOffPeakRate:                0.0442,
		FluxImportStandardDollarsPerKWH:  0.262823,
		FluxImportPeakDollarsPerKWH:      0.366961,
		FluxImportOffPeakDollarsPerKWH:   0.158686,
		FluxExportStandardDollarsPerKWH:  0.109097,
		FluxExportPeakDollarsPerKWH:      0.297536,
		FluxExportOffPeakDollarsPerKWH:   0.046316,
		IntelliFluxStandardDollarsPerKWH: 0.238029,
		IntelliFluxPeakDollarsPerKWH:     0.297536,
		GoPeakRate:                       0.3345,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.378092,
		IOGPeakRate:                      0.2722,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.342829,
		IOGMaxPeakDollarsPerKWH:          0.384184,
		IOGFixedPeakDollarsPerKWH:        0.398567,
		IOGLegacyPeakDollarsPerKWH:       0.350731,
		CosyStandardRate:                 0.2827,
		CosyPeakRate:                     0.4240,
		CosyOffPeakRate:                  0.1386,
		CosyFixedStandardRate:            0.3201,
		CosyFixedPeakRate:                0.4802,
		CosyFixedOffPeakRate:             0.1571,
	},
	"northern_scotland": { // PES P / DNO 17
		RegionCode:                       "P",
		ImportStandardRate:               0.2437,
		ImportPeakRate:                   0.3412,
		ImportOffPeakRate:                0.1463,
		ExportStandardRate:               0.0998,
		ExportPeakRate:                   0.2873,
		ExportOffPeakRate:                0.0473,
		FluxImportStandardDollarsPerKWH:  0.265244,
		FluxImportPeakDollarsPerKWH:      0.370340,
		FluxImportOffPeakDollarsPerKWH:   0.160147,
		FluxExportStandardDollarsPerKWH:  0.110101,
		FluxExportPeakDollarsPerKWH:      0.300276,
		FluxExportOffPeakDollarsPerKWH:   0.046743,
		IntelliFluxStandardDollarsPerKWH: 0.240221,
		IntelliFluxPeakDollarsPerKWH:     0.300276,
		GoPeakRate:                       0.3445,
		GoOffPeakRate:                    0.0940,
		GoFixedPeakDollarsPerKWH:         0.379284,
		IOGPeakRate:                      0.2868,
		IOGOffPeakRate:                   0.0700,
		IOGFlexiblePeakDollarsPerKWH:     0.330278,
		IOGMaxPeakDollarsPerKWH:          0.370723,
		IOGFixedPeakDollarsPerKWH:        0.401026,
		IOGLegacyPeakDollarsPerKWH:       0.360434,
		CosyStandardRate:                 0.2853,
		CosyPeakRate:                     0.4279,
		CosyOffPeakRate:                  0.1399,
		CosyFixedStandardRate:            0.3414,
		CosyFixedPeakRate:                0.5120,
		CosyFixedOffPeakRate:             0.1675,
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

func appendOctopusFluxWindow(
	periods []types.UtilityFeesPeriod,
	start, end time.Time,
	importOffPeakDollarsPerKWH, exportOffPeakDollarsPerKWH float64,
	importPeakDollarsPerKWH, exportPeakDollarsPerKWH float64,
	importStandardDollarsPerKWH, exportStandardDollarsPerKWH float64,
) []types.UtilityFeesPeriod {
	standardHours := []types.UtilityHourPeriod{
		{HourStart: 5, HourEnd: 16},
		{HourStart: 19, HourEnd: 24},
		{HourStart: 0, HourEnd: 2},
	}

	// 1. Off-Peak: 02:00 – 05:00
	periods = append(periods,
		types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Off-Peak",
				Start:       start,
				End:         end,
				LocationPtr: lonLocation,
				Hours:       []types.UtilityHourPeriod{{HourStart: 2, HourEnd: 5}},
			},
			DollarsPerKWH: importOffPeakDollarsPerKWH,
			Description:   "Octopus Flux Off-Peak Rate",
		},
		types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Off-Peak Export",
				Start:       start,
				End:         end,
				LocationPtr: lonLocation,
				Hours:       []types.UtilityHourPeriod{{HourStart: 2, HourEnd: 5}},
			},
			DollarsPerKWH:            exportOffPeakDollarsPerKWH,
			SeparateGenerationCredit: true,
			Description:              "Octopus Flux Off-Peak Export Tariff",
		},
	)

	// 2. Peak: 16:00 – 19:00
	periods = append(periods,
		types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Peak",
				Start:       start,
				End:         end,
				LocationPtr: lonLocation,
				Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
			},
			DollarsPerKWH: importPeakDollarsPerKWH,
			Description:   "Octopus Flux Peak Rate",
		},
		types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Peak Export",
				Start:       start,
				End:         end,
				LocationPtr: lonLocation,
				Hours:       []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 19}},
			},
			DollarsPerKWH:            exportPeakDollarsPerKWH,
			SeparateGenerationCredit: true,
			Description:              "Octopus Flux Peak Export Tariff",
		},
	)

	// 3. Standard / Day: 05:00 – 16:00 and 19:00 – 02:00
	periods = append(periods,
		types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Standard",
				Start:       start,
				End:         end,
				LocationPtr: lonLocation,
				Hours:       standardHours,
			},
			DollarsPerKWH: importStandardDollarsPerKWH,
			Description:   "Octopus Flux Standard Rate",
		},
		types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Standard Export",
				Start:       start,
				End:         end,
				LocationPtr: lonLocation,
				Hours:       standardHours,
			},
			DollarsPerKWH:            exportStandardDollarsPerKWH,
			SeparateGenerationCredit: true,
			Description:              "Octopus Flux Standard Export Tariff",
		},
	)

	return periods
}

func octopusFluxFees(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	_, cfg, err := normalizeOctopusLocation(options.Location)
	if err != nil {
		return nil, err
	}

	fluxRevisionStart := time.Date(2026, time.October, 19, 0, 0, 0, 0, lonLocation)
	periods := make([]types.UtilityFeesPeriod, 0, 12)

	// Pre-2026-10-19 historical Octopus Flux rates.
	periods = appendOctopusFluxWindow(
		periods,
		time.Time{},
		fluxRevisionStart,
		cfg.ImportOffPeakRate,
		cfg.ExportOffPeakRate,
		cfg.ImportPeakRate,
		cfg.ExportPeakRate,
		cfg.ImportStandardRate,
		cfg.ExportStandardRate,
	)

	// Q4 2026 Octopus Flux rates effective 2026-10-19T00:00:00 Europe/London.
	periods = appendOctopusFluxWindow(
		periods,
		fluxRevisionStart,
		time.Time{},
		cfg.FluxImportOffPeakDollarsPerKWH,
		cfg.FluxExportOffPeakDollarsPerKWH,
		cfg.FluxImportPeakDollarsPerKWH,
		cfg.FluxExportPeakDollarsPerKWH,
		cfg.FluxImportStandardDollarsPerKWH,
		cfg.FluxExportStandardDollarsPerKWH,
	)

	return periods, nil
}

func octopusIntelligentFluxFees(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	_, cfg, err := normalizeOctopusLocation(options.Location)
	if err != nil {
		return nil, err
	}

	// Split Standard hours at midnight (00:00-16:00 and 19:00-24:00) so TimePeriod.Contains keeps hStart < hEnd.
	standardHours := []types.UtilityHourPeriod{
		{HourStart: 0, HourEnd: 16},
		{HourStart: 19, HourEnd: 24},
	}
	peakHours := []types.UtilityHourPeriod{
		{HourStart: 16, HourEnd: 19},
	}

	periods := []types.UtilityFeesPeriod{
		// 1. Peak: 16:00 – 19:00 (1:1 import & export parity)
		{
			TimePeriod: types.TimePeriod{
				Name:        "Peak",
				LocationPtr: lonLocation,
				Hours:       peakHours,
			},
			DollarsPerKWH: cfg.IntelliFluxPeakDollarsPerKWH,
			Description:   "Intelligent Octopus Flux Peak Rate",
		},
		{
			TimePeriod: types.TimePeriod{
				Name:        "Peak Export",
				LocationPtr: lonLocation,
				Hours:       peakHours,
			},
			DollarsPerKWH:            cfg.IntelliFluxPeakDollarsPerKWH,
			SeparateGenerationCredit: true,
			Description:              "Intelligent Octopus Flux Peak Export Tariff",
		},
		// 2. Standard / Day: 00:00 – 16:00 and 19:00 – 24:00 (1:1 import & export parity)
		{
			TimePeriod: types.TimePeriod{
				Name:        "Standard",
				LocationPtr: lonLocation,
				Hours:       standardHours,
			},
			DollarsPerKWH: cfg.IntelliFluxStandardDollarsPerKWH,
			Description:   "Intelligent Octopus Flux Standard Rate",
		},
		{
			TimePeriod: types.TimePeriod{
				Name:        "Standard Export",
				LocationPtr: lonLocation,
				Hours:       standardHours,
			},
			DollarsPerKWH:            cfg.IntelliFluxStandardDollarsPerKWH,
			SeparateGenerationCredit: true,
			Description:              "Intelligent Octopus Flux Standard Export Tariff",
		},
	}

	return periods, nil
}

func octopusExportPeriods(options types.UtilityRateOptions) []types.UtilityFeesPeriod {
	exportDollarsPerKWH := octopusOutgoing12pDollarsPerKWH // Default: Outgoing Octopus 12p/kWh (OUTGOING-VAR-24-10-26)
	switch options.GenerationRate {
	case "outgoing_15p":
		exportDollarsPerKWH = octopusOutgoing15pDollarsPerKWH
	case "outgoing_12p":
		exportDollarsPerKWH = octopusOutgoing12pDollarsPerKWH
	case "outgoing_prime", "outgoing_agile", "none":
		exportDollarsPerKWH = 0.0
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
				DollarsPerKWH:            octopusPrimeExportPeakDollarsPerKWH,
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
				DollarsPerKWH:            octopusPrimeExportOffPeakDollarsPerKWH,
				SeparateGenerationCredit: true,
				Description:              "Prime Outgoing Octopus Off-Peak Export Tariff",
			},
		}
	}
	if exportDollarsPerKWH > 0 {
		return []types.UtilityFeesPeriod{
			{
				TimePeriod: types.TimePeriod{
					LocationPtr: lonLocation,
				},
				DollarsPerKWH:            exportDollarsPerKWH,
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

	offPeakDollarsPerKWH := cfg.GoOffPeakRate
	peakDollarsPerKWH := cfg.GoPeakRate
	if options.RateClass == "fixed" {
		offPeakDollarsPerKWH = octopusGoFixedOffPeakDollarsPerKWH
		peakDollarsPerKWH = cfg.GoFixedPeakDollarsPerKWH
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
			DollarsPerKWH: offPeakDollarsPerKWH,
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
			DollarsPerKWH: peakDollarsPerKWH,
			Description:   "Octopus Go Day Rate",
		},
	}

	periods = append(periods, octopusExportPeriods(options)...)
	return periods, nil
}

func appendOctopusIntelligentGoWindow(
	periods []types.UtilityFeesPeriod,
	start, end time.Time,
	offPeakDollarsPerKWH, peakDollarsPerKWH float64,
) []types.UtilityFeesPeriod {
	return append(periods,
		// 1. Overnight Off-Peak: 23:30 – 05:30 (6 hours)
		types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Off-Peak",
				Start:       start,
				End:         end,
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 23, MinuteStart: 30, HourEnd: 24},
					{HourStart: 0, HourEnd: 5, MinuteEnd: 30},
				},
			},
			DollarsPerKWH: offPeakDollarsPerKWH,
			Description:   "Intelligent Octopus Go Off-Peak Rate",
		},
		// 2. Day Peak: 05:30 – 23:30 (18 hours)
		types.UtilityFeesPeriod{
			TimePeriod: types.TimePeriod{
				Name:        "Peak",
				Start:       start,
				End:         end,
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 5, MinuteStart: 30, HourEnd: 23, MinuteEnd: 30},
				},
			},
			DollarsPerKWH: peakDollarsPerKWH,
			Description:   "Intelligent Octopus Go Day Rate",
		},
	)
}

func octopusIntelligentGoFees(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	_, cfg, err := normalizeOctopusLocation(options.Location)
	if err != nil {
		return nil, err
	}

	var periods []types.UtilityFeesPeriod
	switch options.RateClass {
	case "max":
		periods = appendOctopusIntelligentGoWindow(
			periods,
			time.Time{},
			time.Time{},
			octopusIOGMaxOffPeakDollarsPerKWH,
			cfg.IOGMaxPeakDollarsPerKWH,
		)
	case "fixed":
		periods = appendOctopusIntelligentGoWindow(
			periods,
			time.Time{},
			time.Time{},
			octopusIOGFixedOffPeakDollarsPerKWH,
			cfg.IOGFixedPeakDollarsPerKWH,
		)
	case "legacy_24_10_29":
		periods = appendOctopusIntelligentGoWindow(
			periods,
			time.Time{},
			time.Time{},
			octopusIOGLegacyOffPeakDollarsPerKWH,
			cfg.IOGLegacyPeakDollarsPerKWH,
		)
	default:
		// Default flexible (including empty or unrecognized/stale RateClass fallback):
		// preserve pre-2026-11-04 INTELLI-VAR-22-10-14 historical rates and transition
		// to IOG-SMB-VAR-26-11-04 at 2026-11-04T00:00:00 Europe/London.
		iogRevisionStart := time.Date(2026, time.November, 4, 0, 0, 0, 0, lonLocation)
		periods = make([]types.UtilityFeesPeriod, 0, 6)
		periods = appendOctopusIntelligentGoWindow(
			periods,
			time.Time{},
			iogRevisionStart,
			cfg.IOGOffPeakRate,
			cfg.IOGPeakRate,
		)
		periods = appendOctopusIntelligentGoWindow(
			periods,
			iogRevisionStart,
			time.Time{},
			octopusIOGFlexibleOffPeakDollarsPerKWH,
			cfg.IOGFlexiblePeakDollarsPerKWH,
		)
	}

	periods = append(periods, octopusExportPeriods(options)...)
	return periods, nil
}

func octopusCosyFees(options types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
	_, cfg, err := normalizeOctopusLocation(options.Location)
	if err != nil {
		return nil, err
	}

	offPeakRate := cfg.CosyOffPeakRate
	peakRate := cfg.CosyPeakRate
	standardRate := cfg.CosyStandardRate
	if options.RateClass == "fixed" {
		offPeakRate = cfg.CosyFixedOffPeakRate
		peakRate = cfg.CosyFixedPeakRate
		standardRate = cfg.CosyFixedStandardRate
	}

	periods := []types.UtilityFeesPeriod{
		// 1. Off-Peak ("Cosy" hours): 04:00 – 07:00, 13:00 – 16:00, and 22:00 – 00:00 (8 hours)
		{
			TimePeriod: types.TimePeriod{
				Name:        "Off-Peak",
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 4, HourEnd: 7},
					{HourStart: 13, HourEnd: 16},
					{HourStart: 22, HourEnd: 24},
				},
			},
			DollarsPerKWH: offPeakRate,
			Description:   "Cosy Octopus Off-Peak Rate",
		},
		// 2. Peak: 16:00 – 19:00 (3 hours)
		{
			TimePeriod: types.TimePeriod{
				Name:        "Peak",
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 16, HourEnd: 19},
				},
			},
			DollarsPerKWH: peakRate,
			Description:   "Cosy Octopus Peak Rate",
		},
		// 3. Standard / Day: 00:00 – 04:00, 07:00 – 13:00, and 19:00 – 22:00 (13 hours)
		{
			TimePeriod: types.TimePeriod{
				Name:        "Standard",
				LocationPtr: lonLocation,
				Hours: []types.UtilityHourPeriod{
					{HourStart: 0, HourEnd: 4},
					{HourStart: 7, HourEnd: 13},
					{HourStart: 19, HourEnd: 22},
				},
			},
			DollarsPerKWH: standardRate,
			Description:   "Cosy Octopus Standard Rate",
		},
	}

	periods = append(periods, octopusExportPeriods(options)...)
	return periods, nil
}

func octopusLocationOption() types.UtilityRateOption {
	return types.UtilityRateOption{
		Field:       "location",
		Name:        "Distribution Network (DNO / PES Region)",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your regional electricity distribution network area.",
		Choices:     octopusLocationChoices,
		Default:     "london",
	}
}

func octopusGenerationRateOption() types.UtilityRateOption {
	return types.UtilityRateOption{
		Field:       "generationRate",
		Name:        "Export Tariff",
		Type:        types.UtilityOptionTypeSelect,
		Description: "Select your export credit agreement (e.g. Outgoing Octopus).",
		Choices: []types.UtilityOptionChoice{
			{Value: "outgoing_12p", Name: "Outgoing Octopus (12p/kWh)"},
			{Value: "outgoing_15p", Name: "Outgoing Octopus Fixed (15p/kWh)"},
			{Value: "outgoing_prime", Name: "Prime Outgoing Octopus (16p Peak / 9p Off-Peak)"},
			{Value: "outgoing_agile", Name: "Agile Outgoing Octopus"},
			{Value: "none", Name: "No Export Credit"},
		},
		Default: "outgoing_12p",
	}
}

func octopusGoRateOptions() []types.UtilityRateOption {
	return []types.UtilityRateOption{
		{
			Field:       "rateClass",
			Name:        "Tariff Type",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select whether you are on the variable Flexible tariff or the 12M Fixed tariff.",
			Choices: []types.UtilityOptionChoice{
				{Value: "flexible", Name: "Octopus Go (Flexible)"},
				{Value: "fixed", Name: "Octopus Go 12M Fixed"},
			},
			Default: "flexible",
		},
		octopusLocationOption(),
		octopusGenerationRateOption(),
	}
}

func octopusIntelligentGoRateOptions() []types.UtilityRateOption {
	return []types.UtilityRateOption{
		{
			Field:       "rateClass",
			Name:        "Tariff Type",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your Intelligent Octopus Go tariff variant.",
			Choices: []types.UtilityOptionChoice{
				{Value: "flexible", Name: "Intelligent Octopus Go (Flexible)"},
				{Value: "max", Name: "Intelligent Octopus Go Max"},
				{Value: "fixed", Name: "Intelligent Octopus Go 12M Fixed"},
				{Value: "legacy_24_10_29", Name: "Intelligent Octopus Go (Legacy Oct 2024)"},
			},
			Default: "flexible",
		},
		octopusLocationOption(),
		octopusGenerationRateOption(),
	}
}

func octopusCosyRateOptions() []types.UtilityRateOption {
	return []types.UtilityRateOption{
		{
			Field:       "rateClass",
			Name:        "Tariff Type",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select whether you are on the variable Flexible tariff or the 12M Fixed tariff.",
			Choices: []types.UtilityOptionChoice{
				{Value: "flexible", Name: "Cosy Octopus (Flexible)"},
				{Value: "fixed", Name: "Cosy Octopus 12M Fixed"},
			},
			Default: "flexible",
		},
		octopusLocationOption(),
		octopusGenerationRateOption(),
	}
}

// octopusUtilityInfo returns metadata for Octopus Energy (UK).
func octopusUtilityInfo() types.UtilityProviderInfo {
	return types.UtilityProviderInfo{
		ID:   "octopus",
		Name: "Octopus Energy (UK)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "octopus_flux",
				Name:    "Octopus Flux",
				Options: []types.UtilityRateOption{octopusLocationOption()},
				GetFees: octopusFluxFees,
			},
			{
				ID:      "octopus_intelligent_flux",
				Name:    "Intelligent Octopus Flux",
				Options: []types.UtilityRateOption{octopusLocationOption()},
				GetFees: octopusIntelligentFluxFees,
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
				Options: octopusIntelligentGoRateOptions(),
				GetFees: octopusIntelligentGoFees,
			},
			{
				ID:      "octopus_cosy",
				Name:    "Cosy Octopus",
				Options: octopusCosyRateOptions(),
				GetFees: octopusCosyFees,
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
	return settings.UtilityRateOptions.GenerationRate == "outgoing_agile" && r.location == loc
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
