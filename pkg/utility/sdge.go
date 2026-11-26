package utility

// Tariff Audit Metadata
// - Provider(s): sdge (San Diego Gas & Electric (SDG&E))
// - Official Sources:
//   - https://www.sdge.com/total-electric-rates (SDG&E Historical & Current Total Electric Rate Tables: 1/1/26, 4/1/26, 6/1/26, and 8/1/26 Schedules DR, DR-SES, EV-TOU, EV-TOU-2, EV-TOU-5, TOU-DR, TOU-DR1, TOU-DR2, TOU-ELEC)
//   - https://www.sdge.com/residential/pricing-plans (SDG&E Residential Pricing Plans, TOU Hour Windows & Seasonal Transitions)
//   - https://www.sdge.com/sites/default/files/regulatory/8-1-26%20Schedule%20DR%20Total%20Rates%20Table.pdf (Schedule DR Effective 8/1/2026)
//   - https://www.sdge.com/sites/default/files/regulatory/8-1-26%20Schedule%20TOU-ELEC%20Total%20Rates%20Table.pdf (Schedule TOU-ELEC Effective 8/1/2026)
//   - https://sdcommunitypower.org/residential-rates/2021v/ & https://sdcommunitypower.org/residential-rates/2022v/ (San Diego Community Power 2021v & 2022v Residential Generation Schedules)
//   - https://www.sdge.com/sites/default/files/SDCP_SDGE%20JRC%2008.01.2026_Final.pdf (SDCP & SDG&E Joint Rate Comparisons Effective August 1, 2026 / May 1, 2026)
//   - https://www.sdge.com/solar/solar-billing-plan/export-pricing (SDG&E Solar Billing Plan 2026 & 2027 MIDAS Export Pricing Archives: NBT23, NBT24, NBT25, NBT26, NBT27, NBT00)
//   - https://docs.cpuc.ca.gov/PublishedDocs/Published/G000/M499/K921/499921246.PDF (CPUC Decision D.22-12-056 p. 241 confirming $0.00/kWh ACC Plus export adders for SDG&E)
// - Effective Window: 2026-01-01 through 2027-12-31 (1/1/26, 4/1/26, 6/1/26, and 8/1/26 SDG&E rate tables carried through 2027-12-31 until superseded, plus 2026-2027 MIDAS NBT export cohorts)
// - Revision Cadence: Annual (Jan 1 CPUC Consolidated Rate Filing & NBT Export Update + Mid-Year UDC/EECC/PCIA Advice Letters)
// - Next Audit Due: 2027-01-05 (Check Jan 1, 2027 CPUC consolidated residential UDC, EECC, PCIA, and Schedule NEM-ST NBC filings plus May 2027 SDCP rate update)
// - Last Verified: 2026-11-24
// - Modeling Notes: Models Tier 1 volumetric consumption (up to 130% of baseline) on tiered residential schedules (Schedule DR and Schedules TOU-DR, TOU-DR1, TOU-DR2 including -$0.10905, -$0.10892, -$0.10663, and -$0.10702/kWh TRAC baseline adjustment credits) and untiered volumetric rates on EV-TOU, EV-TOU-2, EV-TOU-5, TOU-ELEC, and DR-SES; excludes fixed daily/monthly Base Services Charges ($0.79343/day / ~$24/mo) and minimum bills ($0.413/day); models SDCP CCA generation with 2021v PCIA ($0.03557/$0.03564/$0.03348/kWh for san_diego) and 2022v PCIA ($0.02999/$0.03005/$0.02789/kWh for unincorporated); models NEM 1.0 (1:1 full retail credits), NEM 2.0 (Schedule NEM-ST NBCs of $0.02099/kWh standard or $0.02297/kWh on EV-TOU via GridAdditional with embedded PPP+CTC deducted from base DollarsPerKWH), and Solar Billing Plan / Schedule NBT (2026-2027 576-slot MIDAS export cohorts NBT23/NBT24, NBT25/NBT26, and NBT27/NBT00 with $0.00/kWh ACC Plus).

import (
	"fmt"
	"strings"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

const (
	// Schedule WF-NBC + DWR-BC ($0.00591/kWh) plus UDC-embedded Schedule NEM-ST Non-Bypassable Charges:
	// - Standard residential schedules: PPP ($0.01515) + ND ($0.00000) + CTC (-$0.00007) = $0.01508/kWh embedded in UDC -> $0.02099/kWh total NEM-ST NBC.
	// - Schedule EV-TOU: PPP ($0.01713) + ND ($0.00000) + CTC (-$0.00007) = $0.01706/kWh embedded in UDC -> $0.02297/kWh total NEM-ST NBC.
	sdgeUDCEmbeddedStandardNBCDollarsPerKWH = 0.01508
	sdgeUDCEmbeddedEVTOUNBCDollarsPerKWH    = 0.01706
	sdgeNEMSTStandardNBCDollarsPerKWH       = 0.02099
	sdgeNEMSTEVTOUNBCDollarsPerKWH          = 0.02297
)

// sdgeNBT23Data contains Avoided Cost Calculator (ACC) NBT23 and NBT24 export rates ($/kWh)
// for 2026 and 2027, representing the sum of Generation (USCA-XXSD) and Delivery (USCA-SDXX)
// credit rates from SDG&E MIDAS archives, grouped by Month_DayType_Hour.
var sdgeNBT23Data = map[int]map[string]float64{
	2026: {
		"Apr_Weekday_0":  0.062302,
		"Apr_Weekday_1":  0.055443,
		"Apr_Weekday_10": 0.011765,
		"Apr_Weekday_11": 0.011035,
		"Apr_Weekday_12": 0.010924,
		"Apr_Weekday_13": 0.009882,
		"Apr_Weekday_14": 0.010502,
		"Apr_Weekday_15": 0.007831,
		"Apr_Weekday_16": 0.011684,
		"Apr_Weekday_17": 0.027061,
		"Apr_Weekday_18": 0.066540,
		"Apr_Weekday_19": 0.062338,
		"Apr_Weekday_2":  0.052377,
		"Apr_Weekday_20": 0.059504,
		"Apr_Weekday_21": 0.059576,
		"Apr_Weekday_22": 0.059063,
		"Apr_Weekday_23": 0.058076,
		"Apr_Weekday_3":  0.051120,
		"Apr_Weekday_4":  0.052492,
		"Apr_Weekday_5":  0.055661,
		"Apr_Weekday_6":  0.058477,
		"Apr_Weekday_7":  0.048283,
		"Apr_Weekday_8":  0.014406,
		"Apr_Weekday_9":  0.013068,
		"Apr_Weekend_0":  0.053934,
		"Apr_Weekend_1":  0.050700,
		"Apr_Weekend_10": 0.005789,
		"Apr_Weekend_11": 0.005786,
		"Apr_Weekend_12": 0.009785,
		"Apr_Weekend_13": 0.007054,
		"Apr_Weekend_14": 0.003970,
		"Apr_Weekend_15": 0.002056,
		"Apr_Weekend_16": 0.000312,
		"Apr_Weekend_17": 0.003548,
		"Apr_Weekend_18": 0.071260,
		"Apr_Weekend_19": 0.058396,
		"Apr_Weekend_2":  0.042856,
		"Apr_Weekend_20": 0.055932,
		"Apr_Weekend_21": 0.056268,
		"Apr_Weekend_22": 0.056827,
		"Apr_Weekend_23": 0.062284,
		"Apr_Weekend_3":  0.039569,
		"Apr_Weekend_4":  0.039634,
		"Apr_Weekend_5":  0.045217,
		"Apr_Weekend_6":  0.053954,
		"Apr_Weekend_7":  0.025218,
		"Apr_Weekend_8":  0.005799,
		"Apr_Weekend_9":  0.005786,
		"Aug_Weekday_0":  0.064095,
		"Aug_Weekday_1":  0.060870,
		"Aug_Weekday_10": 0.071032,
		"Aug_Weekday_11": 0.068576,
		"Aug_Weekday_12": 0.067615,
		"Aug_Weekday_13": 0.067980,
		"Aug_Weekday_14": 0.192526,
		"Aug_Weekday_15": 0.317167,
		"Aug_Weekday_16": 0.565540,
		"Aug_Weekday_17": 1.115045,
		"Aug_Weekday_18": 1.424803,
		"Aug_Weekday_19": 2.314067,
		"Aug_Weekday_2":  0.059425,
		"Aug_Weekday_20": 0.962073,
		"Aug_Weekday_21": 0.813178,
		"Aug_Weekday_22": 0.614557,
		"Aug_Weekday_23": 0.063347,
		"Aug_Weekday_3":  0.058886,
		"Aug_Weekday_4":  0.058925,
		"Aug_Weekday_5":  0.059449,
		"Aug_Weekday_6":  0.059704,
		"Aug_Weekday_7":  0.060589,
		"Aug_Weekday_8":  0.063016,
		"Aug_Weekday_9":  0.073160,
		"Aug_Weekend_0":  0.060789,
		"Aug_Weekend_1":  0.060296,
		"Aug_Weekend_10": 0.056166,
		"Aug_Weekend_11": 0.056264,
		"Aug_Weekend_12": 0.055236,
		"Aug_Weekend_13": 0.055116,
		"Aug_Weekend_14": 0.054814,
		"Aug_Weekend_15": 0.055582,
		"Aug_Weekend_16": 0.058742,
		"Aug_Weekend_17": 0.208658,
		"Aug_Weekend_18": 0.499942,
		"Aug_Weekend_19": 1.441894,
		"Aug_Weekend_2":  0.057860,
		"Aug_Weekend_20": 0.540768,
		"Aug_Weekend_21": 0.750717,
		"Aug_Weekend_22": 0.570893,
		"Aug_Weekend_23": 0.067811,
		"Aug_Weekend_3":  0.057554,
		"Aug_Weekend_4":  0.057185,
		"Aug_Weekend_5":  0.056758,
		"Aug_Weekend_6":  0.055472,
		"Aug_Weekend_7":  0.051826,
		"Aug_Weekend_8":  0.054764,
		"Aug_Weekend_9":  0.055752,
		"Dec_Weekday_0":  0.058470,
		"Dec_Weekday_1":  0.058417,
		"Dec_Weekday_10": 0.063210,
		"Dec_Weekday_11": 0.062324,
		"Dec_Weekday_12": 0.060918,
		"Dec_Weekday_13": 0.060295,
		"Dec_Weekday_14": 0.060503,
		"Dec_Weekday_15": 0.062529,
		"Dec_Weekday_16": 0.067806,
		"Dec_Weekday_17": 0.066810,
		"Dec_Weekday_18": 0.066426,
		"Dec_Weekday_19": 0.068309,
		"Dec_Weekday_2":  0.057758,
		"Dec_Weekday_20": 0.069178,
		"Dec_Weekday_21": 0.066800,
		"Dec_Weekday_22": 0.060257,
		"Dec_Weekday_23": 0.060051,
		"Dec_Weekday_3":  0.057367,
		"Dec_Weekday_4":  0.058096,
		"Dec_Weekday_5":  0.058998,
		"Dec_Weekday_6":  0.061412,
		"Dec_Weekday_7":  0.063284,
		"Dec_Weekday_8":  0.064123,
		"Dec_Weekday_9":  0.065175,
		"Dec_Weekend_0":  0.059874,
		"Dec_Weekend_1":  0.059400,
		"Dec_Weekend_10": 0.055711,
		"Dec_Weekend_11": 0.055144,
		"Dec_Weekend_12": 0.053481,
		"Dec_Weekend_13": 0.053413,
		"Dec_Weekend_14": 0.053420,
		"Dec_Weekend_15": 0.054351,
		"Dec_Weekend_16": 0.061462,
		"Dec_Weekend_17": 0.066227,
		"Dec_Weekend_18": 0.068027,
		"Dec_Weekend_19": 0.068114,
		"Dec_Weekend_2":  0.059300,
		"Dec_Weekend_20": 0.065326,
		"Dec_Weekend_21": 0.061603,
		"Dec_Weekend_22": 0.060676,
		"Dec_Weekend_23": 0.061212,
		"Dec_Weekend_3":  0.059053,
		"Dec_Weekend_4":  0.058714,
		"Dec_Weekend_5":  0.058677,
		"Dec_Weekend_6":  0.058946,
		"Dec_Weekend_7":  0.056988,
		"Dec_Weekend_8":  0.055370,
		"Dec_Weekend_9":  0.055231,
		"Feb_Weekday_0":  0.061405,
		"Feb_Weekday_1":  0.061109,
		"Feb_Weekday_10": 0.055228,
		"Feb_Weekday_11": 0.052400,
		"Feb_Weekday_12": 0.051535,
		"Feb_Weekday_13": 0.049897,
		"Feb_Weekday_14": 0.050922,
		"Feb_Weekday_15": 0.054838,
		"Feb_Weekday_16": 0.070129,
		"Feb_Weekday_17": 0.071727,
		"Feb_Weekday_18": 0.068211,
		"Feb_Weekday_19": 0.069171,
		"Feb_Weekday_2":  0.060471,
		"Feb_Weekday_20": 0.068540,
		"Feb_Weekday_21": 0.071599,
		"Feb_Weekday_22": 0.065077,
		"Feb_Weekday_23": 0.062507,
		"Feb_Weekday_3":  0.060292,
		"Feb_Weekday_4":  0.060980,
		"Feb_Weekday_5":  0.061858,
		"Feb_Weekday_6":  0.065529,
		"Feb_Weekday_7":  0.067665,
		"Feb_Weekday_8":  0.059288,
		"Feb_Weekday_9":  0.057354,
		"Feb_Weekend_0":  0.060337,
		"Feb_Weekend_1":  0.060037,
		"Feb_Weekend_10": 0.033281,
		"Feb_Weekend_11": 0.031415,
		"Feb_Weekend_12": 0.032327,
		"Feb_Weekend_13": 0.033330,
		"Feb_Weekend_14": 0.034034,
		"Feb_Weekend_15": 0.033912,
		"Feb_Weekend_16": 0.062974,
		"Feb_Weekend_17": 0.073280,
		"Feb_Weekend_18": 0.073489,
		"Feb_Weekend_19": 0.072799,
		"Feb_Weekend_2":  0.059048,
		"Feb_Weekend_20": 0.072994,
		"Feb_Weekend_21": 0.073087,
		"Feb_Weekend_22": 0.064239,
		"Feb_Weekend_23": 0.065952,
		"Feb_Weekend_3":  0.058560,
		"Feb_Weekend_4":  0.058811,
		"Feb_Weekend_5":  0.059373,
		"Feb_Weekend_6":  0.063250,
		"Feb_Weekend_7":  0.054265,
		"Feb_Weekend_8":  0.036076,
		"Feb_Weekend_9":  0.033529,
		"Jan_Weekday_0":  0.059033,
		"Jan_Weekday_1":  0.059367,
		"Jan_Weekday_10": 0.061957,
		"Jan_Weekday_11": 0.060867,
		"Jan_Weekday_12": 0.060443,
		"Jan_Weekday_13": 0.059803,
		"Jan_Weekday_14": 0.059433,
		"Jan_Weekday_15": 0.061244,
		"Jan_Weekday_16": 0.066992,
		"Jan_Weekday_17": 0.066787,
		"Jan_Weekday_18": 0.066225,
		"Jan_Weekday_19": 0.066745,
		"Jan_Weekday_2":  0.058586,
		"Jan_Weekday_20": 0.066608,
		"Jan_Weekday_21": 0.066149,
		"Jan_Weekday_22": 0.060866,
		"Jan_Weekday_23": 0.059664,
		"Jan_Weekday_3":  0.058161,
		"Jan_Weekday_4":  0.058723,
		"Jan_Weekday_5":  0.059445,
		"Jan_Weekday_6":  0.062015,
		"Jan_Weekday_7":  0.063336,
		"Jan_Weekday_8":  0.062698,
		"Jan_Weekday_9":  0.063502,
		"Jan_Weekend_0":  0.058019,
		"Jan_Weekend_1":  0.056908,
		"Jan_Weekend_10": 0.048392,
		"Jan_Weekend_11": 0.047876,
		"Jan_Weekend_12": 0.046180,
		"Jan_Weekend_13": 0.047655,
		"Jan_Weekend_14": 0.049811,
		"Jan_Weekend_15": 0.051466,
		"Jan_Weekend_16": 0.060688,
		"Jan_Weekend_17": 0.065896,
		"Jan_Weekend_18": 0.066233,
		"Jan_Weekend_19": 0.066403,
		"Jan_Weekend_2":  0.057361,
		"Jan_Weekend_20": 0.066618,
		"Jan_Weekend_21": 0.065257,
		"Jan_Weekend_22": 0.060544,
		"Jan_Weekend_23": 0.059103,
		"Jan_Weekend_3":  0.056379,
		"Jan_Weekend_4":  0.056084,
		"Jan_Weekend_5":  0.053807,
		"Jan_Weekend_6":  0.055035,
		"Jan_Weekend_7":  0.056278,
		"Jan_Weekend_8":  0.050914,
		"Jan_Weekend_9":  0.048603,
		"Jul_Weekday_0":  0.064280,
		"Jul_Weekday_1":  0.055837,
		"Jul_Weekday_10": 0.061448,
		"Jul_Weekday_11": 0.060491,
		"Jul_Weekday_12": 0.060049,
		"Jul_Weekday_13": 0.060498,
		"Jul_Weekday_14": 0.060413,
		"Jul_Weekday_15": 0.060289,
		"Jul_Weekday_16": 0.063383,
		"Jul_Weekday_17": 0.195864,
		"Jul_Weekday_18": 0.308001,
		"Jul_Weekday_19": 0.549231,
		"Jul_Weekday_2":  0.055593,
		"Jul_Weekday_20": 0.178145,
		"Jul_Weekday_21": 0.076315,
		"Jul_Weekday_22": 0.074016,
		"Jul_Weekday_23": 0.062048,
		"Jul_Weekday_3":  0.055697,
		"Jul_Weekday_4":  0.055730,
		"Jul_Weekday_5":  0.055758,
		"Jul_Weekday_6":  0.056098,
		"Jul_Weekday_7":  0.057734,
		"Jul_Weekday_8":  0.059105,
		"Jul_Weekday_9":  0.061884,
		"Jul_Weekend_0":  0.062569,
		"Jul_Weekend_1":  0.058101,
		"Jul_Weekend_10": 0.039817,
		"Jul_Weekend_11": 0.035851,
		"Jul_Weekend_12": 0.034312,
		"Jul_Weekend_13": 0.032265,
		"Jul_Weekend_14": 0.034496,
		"Jul_Weekend_15": 0.035372,
		"Jul_Weekend_16": 0.036633,
		"Jul_Weekend_17": 0.066860,
		"Jul_Weekend_18": 0.110717,
		"Jul_Weekend_19": 0.767030,
		"Jul_Weekend_2":  0.057611,
		"Jul_Weekend_20": 0.261989,
		"Jul_Weekend_21": 0.078122,
		"Jul_Weekend_22": 0.075334,
		"Jul_Weekend_23": 0.074424,
		"Jul_Weekend_3":  0.056682,
		"Jul_Weekend_4":  0.056477,
		"Jul_Weekend_5":  0.056159,
		"Jul_Weekend_6":  0.056115,
		"Jul_Weekend_7":  0.043221,
		"Jul_Weekend_8":  0.041742,
		"Jul_Weekend_9":  0.041134,
		"Jun_Weekday_0":  0.057626,
		"Jun_Weekday_1":  0.052476,
		"Jun_Weekday_10": 0.054767,
		"Jun_Weekday_11": 0.053806,
		"Jun_Weekday_12": 0.053358,
		"Jun_Weekday_13": 0.053305,
		"Jun_Weekday_14": 0.053389,
		"Jun_Weekday_15": 0.054140,
		"Jun_Weekday_16": 0.055395,
		"Jun_Weekday_17": 0.420398,
		"Jun_Weekday_18": 0.423794,
		"Jun_Weekday_19": 0.192794,
		"Jun_Weekday_2":  0.052747,
		"Jun_Weekday_20": 0.077827,
		"Jun_Weekday_21": 0.070348,
		"Jun_Weekday_22": 0.069465,
		"Jun_Weekday_23": 0.061798,
		"Jun_Weekday_3":  0.053229,
		"Jun_Weekday_4":  0.052727,
		"Jun_Weekday_5":  0.053540,
		"Jun_Weekday_6":  0.054072,
		"Jun_Weekday_7":  0.055064,
		"Jun_Weekday_8":  0.055229,
		"Jun_Weekday_9":  0.056199,
		"Jun_Weekend_0":  0.058300,
		"Jun_Weekend_1":  0.054212,
		"Jun_Weekend_10": 0.040587,
		"Jun_Weekend_11": 0.038883,
		"Jun_Weekend_12": 0.036511,
		"Jun_Weekend_13": 0.034324,
		"Jun_Weekend_14": 0.034599,
		"Jun_Weekend_15": 0.036793,
		"Jun_Weekend_16": 0.037472,
		"Jun_Weekend_17": 0.063211,
		"Jun_Weekend_18": 0.071504,
		"Jun_Weekend_19": 0.072801,
		"Jun_Weekend_2":  0.055422,
		"Jun_Weekend_20": 0.074195,
		"Jun_Weekend_21": 0.074259,
		"Jun_Weekend_22": 0.072892,
		"Jun_Weekend_23": 0.072218,
		"Jun_Weekend_3":  0.056823,
		"Jun_Weekend_4":  0.056309,
		"Jun_Weekend_5":  0.056064,
		"Jun_Weekend_6":  0.058170,
		"Jun_Weekend_7":  0.047456,
		"Jun_Weekend_8":  0.038447,
		"Jun_Weekend_9":  0.039422,
		"Mar_Weekday_0":  0.054727,
		"Mar_Weekday_1":  0.051545,
		"Mar_Weekday_10": 0.041091,
		"Mar_Weekday_11": 0.039142,
		"Mar_Weekday_12": 0.037444,
		"Mar_Weekday_13": 0.036386,
		"Mar_Weekday_14": 0.036785,
		"Mar_Weekday_15": 0.036728,
		"Mar_Weekday_16": 0.041689,
		"Mar_Weekday_17": 0.054961,
		"Mar_Weekday_18": 0.067758,
		"Mar_Weekday_19": 0.068320,
		"Mar_Weekday_2":  0.053641,
		"Mar_Weekday_20": 0.065447,
		"Mar_Weekday_21": 0.064782,
		"Mar_Weekday_22": 0.063392,
		"Mar_Weekday_23": 0.055002,
		"Mar_Weekday_3":  0.052973,
		"Mar_Weekday_4":  0.053008,
		"Mar_Weekday_5":  0.052852,
		"Mar_Weekday_6":  0.057721,
		"Mar_Weekday_7":  0.058136,
		"Mar_Weekday_8":  0.050521,
		"Mar_Weekday_9":  0.042775,
		"Mar_Weekend_0":  0.051628,
		"Mar_Weekend_1":  0.051679,
		"Mar_Weekend_10": 0.013687,
		"Mar_Weekend_11": 0.013703,
		"Mar_Weekend_12": 0.014086,
		"Mar_Weekend_13": 0.008441,
		"Mar_Weekend_14": 0.007341,
		"Mar_Weekend_15": 0.009912,
		"Mar_Weekend_16": 0.013212,
		"Mar_Weekend_17": 0.047439,
		"Mar_Weekend_18": 0.070384,
		"Mar_Weekend_19": 0.071626,
		"Mar_Weekend_2":  0.054494,
		"Mar_Weekend_20": 0.069747,
		"Mar_Weekend_21": 0.067984,
		"Mar_Weekend_22": 0.065810,
		"Mar_Weekend_23": 0.060762,
		"Mar_Weekend_3":  0.052004,
		"Mar_Weekend_4":  0.051045,
		"Mar_Weekend_5":  0.049952,
		"Mar_Weekend_6":  0.048754,
		"Mar_Weekend_7":  0.039342,
		"Mar_Weekend_8":  0.019190,
		"Mar_Weekend_9":  0.015650,
		"May_Weekday_0":  0.065664,
		"May_Weekday_1":  0.060074,
		"May_Weekday_10": 0.029585,
		"May_Weekday_11": 0.028635,
		"May_Weekday_12": 0.026593,
		"May_Weekday_13": 0.026734,
		"May_Weekday_14": 0.027348,
		"May_Weekday_15": 0.027092,
		"May_Weekday_16": 0.029996,
		"May_Weekday_17": 0.043460,
		"May_Weekday_18": 0.063588,
		"May_Weekday_19": 0.063671,
		"May_Weekday_2":  0.059305,
		"May_Weekday_20": 0.062619,
		"May_Weekday_21": 0.062262,
		"May_Weekday_22": 0.062899,
		"May_Weekday_23": 0.063092,
		"May_Weekday_3":  0.058912,
		"May_Weekday_4":  0.060060,
		"May_Weekday_5":  0.063157,
		"May_Weekday_6":  0.065161,
		"May_Weekday_7":  0.047460,
		"May_Weekday_8":  0.031890,
		"May_Weekday_9":  0.031492,
		"May_Weekend_0":  0.062208,
		"May_Weekend_1":  0.059123,
		"May_Weekend_10": 0.009084,
		"May_Weekend_11": 0.009056,
		"May_Weekend_12": 0.009359,
		"May_Weekend_13": 0.010803,
		"May_Weekend_14": 0.008740,
		"May_Weekend_15": 0.007445,
		"May_Weekend_16": 0.004861,
		"May_Weekend_17": 0.013091,
		"May_Weekend_18": 0.061293,
		"May_Weekend_19": 0.063066,
		"May_Weekend_2":  0.057648,
		"May_Weekend_20": 0.061931,
		"May_Weekend_21": 0.062663,
		"May_Weekend_22": 0.064713,
		"May_Weekend_23": 0.063523,
		"May_Weekend_3":  0.056114,
		"May_Weekend_4":  0.057774,
		"May_Weekend_5":  0.058710,
		"May_Weekend_6":  0.050628,
		"May_Weekend_7":  0.011696,
		"May_Weekend_8":  0.009200,
		"May_Weekend_9":  0.009200,
		"Nov_Weekday_0":  0.054889,
		"Nov_Weekday_1":  0.055056,
		"Nov_Weekday_10": 0.058152,
		"Nov_Weekday_11": 0.057469,
		"Nov_Weekday_12": 0.056687,
		"Nov_Weekday_13": 0.058556,
		"Nov_Weekday_14": 0.058232,
		"Nov_Weekday_15": 0.061909,
		"Nov_Weekday_16": 0.066026,
		"Nov_Weekday_17": 0.062739,
		"Nov_Weekday_18": 0.061585,
		"Nov_Weekday_19": 0.062872,
		"Nov_Weekday_2":  0.054621,
		"Nov_Weekday_20": 0.064176,
		"Nov_Weekday_21": 0.063280,
		"Nov_Weekday_22": 0.056963,
		"Nov_Weekday_23": 0.056339,
		"Nov_Weekday_3":  0.054491,
		"Nov_Weekday_4":  0.054691,
		"Nov_Weekday_5":  0.055768,
		"Nov_Weekday_6":  0.058278,
		"Nov_Weekday_7":  0.057699,
		"Nov_Weekday_8":  0.058783,
		"Nov_Weekday_9":  0.058868,
		"Nov_Weekend_0":  0.054766,
		"Nov_Weekend_1":  0.053665,
		"Nov_Weekend_10": 0.041659,
		"Nov_Weekend_11": 0.041509,
		"Nov_Weekend_12": 0.041704,
		"Nov_Weekend_13": 0.041106,
		"Nov_Weekend_14": 0.043782,
		"Nov_Weekend_15": 0.050270,
		"Nov_Weekend_16": 0.064386,
		"Nov_Weekend_17": 0.067982,
		"Nov_Weekend_18": 0.066119,
		"Nov_Weekend_19": 0.067445,
		"Nov_Weekend_2":  0.052824,
		"Nov_Weekend_20": 0.066669,
		"Nov_Weekend_21": 0.063528,
		"Nov_Weekend_22": 0.059695,
		"Nov_Weekend_23": 0.058870,
		"Nov_Weekend_3":  0.052305,
		"Nov_Weekend_4":  0.051452,
		"Nov_Weekend_5":  0.051085,
		"Nov_Weekend_6":  0.050685,
		"Nov_Weekend_7":  0.041948,
		"Nov_Weekend_8":  0.041457,
		"Nov_Weekend_9":  0.041181,
		"Oct_Weekday_0":  0.067413,
		"Oct_Weekday_1":  0.056370,
		"Oct_Weekday_10": 0.058373,
		"Oct_Weekday_11": 0.057770,
		"Oct_Weekday_12": 0.057439,
		"Oct_Weekday_13": 0.056969,
		"Oct_Weekday_14": 0.058737,
		"Oct_Weekday_15": 0.296541,
		"Oct_Weekday_16": 0.417935,
		"Oct_Weekday_17": 0.434204,
		"Oct_Weekday_18": 0.421258,
		"Oct_Weekday_19": 0.421311,
		"Oct_Weekday_2":  0.056746,
		"Oct_Weekday_20": 0.066755,
		"Oct_Weekday_21": 0.063875,
		"Oct_Weekday_22": 0.059379,
		"Oct_Weekday_23": 0.057266,
		"Oct_Weekday_3":  0.056540,
		"Oct_Weekday_4":  0.056436,
		"Oct_Weekday_5":  0.056972,
		"Oct_Weekday_6":  0.057497,
		"Oct_Weekday_7":  0.059273,
		"Oct_Weekday_8":  0.056881,
		"Oct_Weekday_9":  0.057808,
		"Oct_Weekend_0":  0.056404,
		"Oct_Weekend_1":  0.056232,
		"Oct_Weekend_10": 0.043694,
		"Oct_Weekend_11": 0.044458,
		"Oct_Weekend_12": 0.044413,
		"Oct_Weekend_13": 0.042184,
		"Oct_Weekend_14": 0.042295,
		"Oct_Weekend_15": 0.045486,
		"Oct_Weekend_16": 0.049855,
		"Oct_Weekend_17": 0.080842,
		"Oct_Weekend_18": 0.073035,
		"Oct_Weekend_19": 0.073962,
		"Oct_Weekend_2":  0.054982,
		"Oct_Weekend_20": 0.070672,
		"Oct_Weekend_21": 0.070388,
		"Oct_Weekend_22": 0.068163,
		"Oct_Weekend_23": 0.066413,
		"Oct_Weekend_3":  0.055055,
		"Oct_Weekend_4":  0.053967,
		"Oct_Weekend_5":  0.053651,
		"Oct_Weekend_6":  0.053148,
		"Oct_Weekend_7":  0.049543,
		"Oct_Weekend_8":  0.043254,
		"Oct_Weekend_9":  0.043443,
		"Sep_Weekday_0":  0.068813,
		"Sep_Weekday_1":  0.055990,
		"Sep_Weekday_10": 0.058399,
		"Sep_Weekday_11": 0.057629,
		"Sep_Weekday_12": 0.056936,
		"Sep_Weekday_13": 0.057784,
		"Sep_Weekday_14": 0.181123,
		"Sep_Weekday_15": 0.313772,
		"Sep_Weekday_16": 0.327124,
		"Sep_Weekday_17": 0.352694,
		"Sep_Weekday_18": 2.374264,
		"Sep_Weekday_19": 2.650945,
		"Sep_Weekday_2":  0.055916,
		"Sep_Weekday_20": 0.275303,
		"Sep_Weekday_21": 0.070560,
		"Sep_Weekday_22": 0.066946,
		"Sep_Weekday_23": 0.057995,
		"Sep_Weekday_3":  0.055586,
		"Sep_Weekday_4":  0.055418,
		"Sep_Weekday_5":  0.055806,
		"Sep_Weekday_6":  0.057424,
		"Sep_Weekday_7":  0.058748,
		"Sep_Weekday_8":  0.056157,
		"Sep_Weekday_9":  0.059596,
		"Sep_Weekend_0":  0.058091,
		"Sep_Weekend_1":  0.057040,
		"Sep_Weekend_10": 0.049689,
		"Sep_Weekend_11": 0.049057,
		"Sep_Weekend_12": 0.046216,
		"Sep_Weekend_13": 0.043430,
		"Sep_Weekend_14": 0.043798,
		"Sep_Weekend_15": 0.046859,
		"Sep_Weekend_16": 0.340696,
		"Sep_Weekend_17": 0.364880,
		"Sep_Weekend_18": 2.151920,
		"Sep_Weekend_19": 2.403966,
		"Sep_Weekend_2":  0.056160,
		"Sep_Weekend_20": 0.146752,
		"Sep_Weekend_21": 0.072746,
		"Sep_Weekend_22": 0.068088,
		"Sep_Weekend_23": 0.058106,
		"Sep_Weekend_3":  0.056143,
		"Sep_Weekend_4":  0.055256,
		"Sep_Weekend_5":  0.055047,
		"Sep_Weekend_6":  0.054113,
		"Sep_Weekend_7":  0.050323,
		"Sep_Weekend_8":  0.046577,
		"Sep_Weekend_9":  0.048450,
	},
	2027: {
		"Apr_Weekday_0":  0.067889,
		"Apr_Weekday_1":  0.056758,
		"Apr_Weekday_10": 0.012716,
		"Apr_Weekday_11": 0.012109,
		"Apr_Weekday_12": 0.011313,
		"Apr_Weekday_13": 0.010053,
		"Apr_Weekday_14": 0.011815,
		"Apr_Weekday_15": 0.008896,
		"Apr_Weekday_16": 0.012705,
		"Apr_Weekday_17": 0.029478,
		"Apr_Weekday_18": 0.065808,
		"Apr_Weekday_19": 0.063563,
		"Apr_Weekday_2":  0.055224,
		"Apr_Weekday_20": 0.061839,
		"Apr_Weekday_21": 0.061981,
		"Apr_Weekday_22": 0.062566,
		"Apr_Weekday_23": 0.063119,
		"Apr_Weekday_3":  0.054449,
		"Apr_Weekday_4":  0.056664,
		"Apr_Weekday_5":  0.059094,
		"Apr_Weekday_6":  0.061670,
		"Apr_Weekday_7":  0.050592,
		"Apr_Weekday_8":  0.016062,
		"Apr_Weekday_9":  0.014323,
		"Apr_Weekend_0":  0.057608,
		"Apr_Weekend_1":  0.057198,
		"Apr_Weekend_10": 0.005675,
		"Apr_Weekend_11": 0.005675,
		"Apr_Weekend_12": 0.006272,
		"Apr_Weekend_13": 0.008126,
		"Apr_Weekend_14": 0.003396,
		"Apr_Weekend_15": 0.002933,
		"Apr_Weekend_16": 0.000322,
		"Apr_Weekend_17": 0.004002,
		"Apr_Weekend_18": 0.077417,
		"Apr_Weekend_19": 0.060141,
		"Apr_Weekend_2":  0.047816,
		"Apr_Weekend_20": 0.058564,
		"Apr_Weekend_21": 0.058592,
		"Apr_Weekend_22": 0.058601,
		"Apr_Weekend_23": 0.064239,
		"Apr_Weekend_3":  0.043004,
		"Apr_Weekend_4":  0.044361,
		"Apr_Weekend_5":  0.050162,
		"Apr_Weekend_6":  0.057199,
		"Apr_Weekend_7":  0.022723,
		"Apr_Weekend_8":  0.005675,
		"Apr_Weekend_9":  0.005675,
		"Aug_Weekday_0":  0.082644,
		"Aug_Weekday_1":  0.075311,
		"Aug_Weekday_10": 0.078771,
		"Aug_Weekday_11": 0.074048,
		"Aug_Weekday_12": 0.073600,
		"Aug_Weekday_13": 0.076958,
		"Aug_Weekday_14": 0.205638,
		"Aug_Weekday_15": 0.336951,
		"Aug_Weekday_16": 0.600645,
		"Aug_Weekday_17": 1.210165,
		"Aug_Weekday_18": 1.616289,
		"Aug_Weekday_19": 2.770618,
		"Aug_Weekday_2":  0.065472,
		"Aug_Weekday_20": 1.200112,
		"Aug_Weekday_21": 1.092120,
		"Aug_Weekday_22": 0.794285,
		"Aug_Weekday_23": 0.087873,
		"Aug_Weekday_3":  0.062438,
		"Aug_Weekday_4":  0.061299,
		"Aug_Weekday_5":  0.061626,
		"Aug_Weekday_6":  0.061991,
		"Aug_Weekday_7":  0.064303,
		"Aug_Weekday_8":  0.069197,
		"Aug_Weekday_9":  0.079283,
		"Aug_Weekend_0":  0.077441,
		"Aug_Weekend_1":  0.073175,
		"Aug_Weekend_10": 0.059342,
		"Aug_Weekend_11": 0.061055,
		"Aug_Weekend_12": 0.059911,
		"Aug_Weekend_13": 0.064052,
		"Aug_Weekend_14": 0.063213,
		"Aug_Weekend_15": 0.068780,
		"Aug_Weekend_16": 0.072342,
		"Aug_Weekend_17": 0.252128,
		"Aug_Weekend_18": 0.612108,
		"Aug_Weekend_19": 1.770584,
		"Aug_Weekend_2":  0.065383,
		"Aug_Weekend_20": 0.673731,
		"Aug_Weekend_21": 0.943112,
		"Aug_Weekend_22": 0.708608,
		"Aug_Weekend_23": 0.082750,
		"Aug_Weekend_3":  0.059922,
		"Aug_Weekend_4":  0.059472,
		"Aug_Weekend_5":  0.058850,
		"Aug_Weekend_6":  0.057587,
		"Aug_Weekend_7":  0.053867,
		"Aug_Weekend_8":  0.057132,
		"Aug_Weekend_9":  0.058901,
		"Dec_Weekday_0":  0.061248,
		"Dec_Weekday_1":  0.061440,
		"Dec_Weekday_10": 0.066046,
		"Dec_Weekday_11": 0.065891,
		"Dec_Weekday_12": 0.064627,
		"Dec_Weekday_13": 0.064546,
		"Dec_Weekday_14": 0.064186,
		"Dec_Weekday_15": 0.066872,
		"Dec_Weekday_16": 0.071970,
		"Dec_Weekday_17": 0.070223,
		"Dec_Weekday_18": 0.070086,
		"Dec_Weekday_19": 0.071183,
		"Dec_Weekday_2":  0.060984,
		"Dec_Weekday_20": 0.072212,
		"Dec_Weekday_21": 0.071324,
		"Dec_Weekday_22": 0.062862,
		"Dec_Weekday_23": 0.062333,
		"Dec_Weekday_3":  0.060589,
		"Dec_Weekday_4":  0.060916,
		"Dec_Weekday_5":  0.061425,
		"Dec_Weekday_6":  0.063795,
		"Dec_Weekday_7":  0.066572,
		"Dec_Weekday_8":  0.068622,
		"Dec_Weekday_9":  0.068149,
		"Dec_Weekend_0":  0.062317,
		"Dec_Weekend_1":  0.061967,
		"Dec_Weekend_10": 0.060311,
		"Dec_Weekend_11": 0.060416,
		"Dec_Weekend_12": 0.059013,
		"Dec_Weekend_13": 0.058433,
		"Dec_Weekend_14": 0.058207,
		"Dec_Weekend_15": 0.059169,
		"Dec_Weekend_16": 0.063406,
		"Dec_Weekend_17": 0.068822,
		"Dec_Weekend_18": 0.069674,
		"Dec_Weekend_19": 0.070317,
		"Dec_Weekend_2":  0.061992,
		"Dec_Weekend_20": 0.068733,
		"Dec_Weekend_21": 0.064914,
		"Dec_Weekend_22": 0.062894,
		"Dec_Weekend_23": 0.064089,
		"Dec_Weekend_3":  0.061934,
		"Dec_Weekend_4":  0.061322,
		"Dec_Weekend_5":  0.061342,
		"Dec_Weekend_6":  0.061743,
		"Dec_Weekend_7":  0.060594,
		"Dec_Weekend_8":  0.059904,
		"Dec_Weekend_9":  0.059979,
		"Feb_Weekday_0":  0.061776,
		"Feb_Weekday_1":  0.061971,
		"Feb_Weekday_10": 0.057264,
		"Feb_Weekday_11": 0.056306,
		"Feb_Weekday_12": 0.054440,
		"Feb_Weekday_13": 0.052410,
		"Feb_Weekday_14": 0.053420,
		"Feb_Weekday_15": 0.056909,
		"Feb_Weekday_16": 0.075072,
		"Feb_Weekday_17": 0.075281,
		"Feb_Weekday_18": 0.070150,
		"Feb_Weekday_19": 0.071342,
		"Feb_Weekday_2":  0.061282,
		"Feb_Weekday_20": 0.071185,
		"Feb_Weekday_21": 0.074884,
		"Feb_Weekday_22": 0.067200,
		"Feb_Weekday_23": 0.064301,
		"Feb_Weekday_3":  0.061371,
		"Feb_Weekday_4":  0.061738,
		"Feb_Weekday_5":  0.063563,
		"Feb_Weekday_6":  0.066959,
		"Feb_Weekday_7":  0.073178,
		"Feb_Weekday_8":  0.060879,
		"Feb_Weekday_9":  0.059745,
		"Feb_Weekend_0":  0.059234,
		"Feb_Weekend_1":  0.058978,
		"Feb_Weekend_10": 0.033399,
		"Feb_Weekend_11": 0.030496,
		"Feb_Weekend_12": 0.032221,
		"Feb_Weekend_13": 0.032578,
		"Feb_Weekend_14": 0.033822,
		"Feb_Weekend_15": 0.041902,
		"Feb_Weekend_16": 0.061645,
		"Feb_Weekend_17": 0.075848,
		"Feb_Weekend_18": 0.077599,
		"Feb_Weekend_19": 0.076874,
		"Feb_Weekend_2":  0.057925,
		"Feb_Weekend_20": 0.077441,
		"Feb_Weekend_21": 0.075357,
		"Feb_Weekend_22": 0.064264,
		"Feb_Weekend_23": 0.062776,
		"Feb_Weekend_3":  0.057570,
		"Feb_Weekend_4":  0.057887,
		"Feb_Weekend_5":  0.058214,
		"Feb_Weekend_6":  0.062342,
		"Feb_Weekend_7":  0.055065,
		"Feb_Weekend_8":  0.039763,
		"Feb_Weekend_9":  0.036059,
		"Jan_Weekday_0":  0.061006,
		"Jan_Weekday_1":  0.061383,
		"Jan_Weekday_10": 0.065655,
		"Jan_Weekday_11": 0.064237,
		"Jan_Weekday_12": 0.063551,
		"Jan_Weekday_13": 0.062992,
		"Jan_Weekday_14": 0.062070,
		"Jan_Weekday_15": 0.063145,
		"Jan_Weekday_16": 0.069458,
		"Jan_Weekday_17": 0.070653,
		"Jan_Weekday_18": 0.069619,
		"Jan_Weekday_19": 0.070019,
		"Jan_Weekday_2":  0.060332,
		"Jan_Weekday_20": 0.070462,
		"Jan_Weekday_21": 0.069693,
		"Jan_Weekday_22": 0.063938,
		"Jan_Weekday_23": 0.061706,
		"Jan_Weekday_3":  0.060228,
		"Jan_Weekday_4":  0.060647,
		"Jan_Weekday_5":  0.061703,
		"Jan_Weekday_6":  0.065094,
		"Jan_Weekday_7":  0.066435,
		"Jan_Weekday_8":  0.065240,
		"Jan_Weekday_9":  0.065828,
		"Jan_Weekend_0":  0.059385,
		"Jan_Weekend_1":  0.059058,
		"Jan_Weekend_10": 0.052036,
		"Jan_Weekend_11": 0.051490,
		"Jan_Weekend_12": 0.050715,
		"Jan_Weekend_13": 0.051164,
		"Jan_Weekend_14": 0.053231,
		"Jan_Weekend_15": 0.054706,
		"Jan_Weekend_16": 0.063139,
		"Jan_Weekend_17": 0.068556,
		"Jan_Weekend_18": 0.070105,
		"Jan_Weekend_19": 0.069617,
		"Jan_Weekend_2":  0.059071,
		"Jan_Weekend_20": 0.069257,
		"Jan_Weekend_21": 0.067389,
		"Jan_Weekend_22": 0.062165,
		"Jan_Weekend_23": 0.060377,
		"Jan_Weekend_3":  0.058065,
		"Jan_Weekend_4":  0.057617,
		"Jan_Weekend_5":  0.056506,
		"Jan_Weekend_6":  0.057157,
		"Jan_Weekend_7":  0.058818,
		"Jan_Weekend_8":  0.054897,
		"Jan_Weekend_9":  0.053116,
		"Jul_Weekday_0":  0.077219,
		"Jul_Weekday_1":  0.064661,
		"Jul_Weekday_10": 0.065832,
		"Jul_Weekday_11": 0.065143,
		"Jul_Weekday_12": 0.065391,
		"Jul_Weekday_13": 0.066829,
		"Jul_Weekday_14": 0.068502,
		"Jul_Weekday_15": 0.069844,
		"Jul_Weekday_16": 0.074517,
		"Jul_Weekday_17": 0.215616,
		"Jul_Weekday_18": 0.337737,
		"Jul_Weekday_19": 0.683470,
		"Jul_Weekday_2":  0.061201,
		"Jul_Weekday_20": 0.247324,
		"Jul_Weekday_21": 0.102988,
		"Jul_Weekday_22": 0.089420,
		"Jul_Weekday_23": 0.084213,
		"Jul_Weekday_3":  0.058857,
		"Jul_Weekday_4":  0.057982,
		"Jul_Weekday_5":  0.057742,
		"Jul_Weekday_6":  0.057997,
		"Jul_Weekday_7":  0.061523,
		"Jul_Weekday_8":  0.063927,
		"Jul_Weekday_9":  0.065873,
		"Jul_Weekend_0":  0.082360,
		"Jul_Weekend_1":  0.070206,
		"Jul_Weekend_10": 0.044236,
		"Jul_Weekend_11": 0.042541,
		"Jul_Weekend_12": 0.040983,
		"Jul_Weekend_13": 0.040867,
		"Jul_Weekend_14": 0.045020,
		"Jul_Weekend_15": 0.048103,
		"Jul_Weekend_16": 0.050241,
		"Jul_Weekend_17": 0.080065,
		"Jul_Weekend_18": 0.109586,
		"Jul_Weekend_19": 0.959624,
		"Jul_Weekend_2":  0.066641,
		"Jul_Weekend_20": 0.321629,
		"Jul_Weekend_21": 0.101675,
		"Jul_Weekend_22": 0.083473,
		"Jul_Weekend_23": 0.096446,
		"Jul_Weekend_3":  0.060293,
		"Jul_Weekend_4":  0.060027,
		"Jul_Weekend_5":  0.059204,
		"Jul_Weekend_6":  0.059848,
		"Jul_Weekend_7":  0.045683,
		"Jul_Weekend_8":  0.042499,
		"Jul_Weekend_9":  0.043228,
		"Jun_Weekday_0":  0.070959,
		"Jun_Weekday_1":  0.057955,
		"Jun_Weekday_10": 0.059671,
		"Jun_Weekday_11": 0.059124,
		"Jun_Weekday_12": 0.058943,
		"Jun_Weekday_13": 0.060057,
		"Jun_Weekday_14": 0.061717,
		"Jun_Weekday_15": 0.064317,
		"Jun_Weekday_16": 0.067025,
		"Jun_Weekday_17": 0.441623,
		"Jun_Weekday_18": 0.449129,
		"Jun_Weekday_19": 0.235368,
		"Jun_Weekday_2":  0.055609,
		"Jun_Weekday_20": 0.134888,
		"Jun_Weekday_21": 0.095896,
		"Jun_Weekday_22": 0.083544,
		"Jun_Weekday_23": 0.079111,
		"Jun_Weekday_3":  0.055015,
		"Jun_Weekday_4":  0.054527,
		"Jun_Weekday_5":  0.055820,
		"Jun_Weekday_6":  0.056352,
		"Jun_Weekday_7":  0.058057,
		"Jun_Weekday_8":  0.059969,
		"Jun_Weekday_9":  0.060563,
		"Jun_Weekend_0":  0.067331,
		"Jun_Weekend_1":  0.058794,
		"Jun_Weekend_10": 0.044212,
		"Jun_Weekend_11": 0.044144,
		"Jun_Weekend_12": 0.043125,
		"Jun_Weekend_13": 0.043596,
		"Jun_Weekend_14": 0.045268,
		"Jun_Weekend_15": 0.048466,
		"Jun_Weekend_16": 0.046967,
		"Jun_Weekend_17": 0.069281,
		"Jun_Weekend_18": 0.079299,
		"Jun_Weekend_19": 0.079416,
		"Jun_Weekend_2":  0.060231,
		"Jun_Weekend_20": 0.089981,
		"Jun_Weekend_21": 0.099992,
		"Jun_Weekend_22": 0.089862,
		"Jun_Weekend_23": 0.097849,
		"Jun_Weekend_3":  0.060768,
		"Jun_Weekend_4":  0.057112,
		"Jun_Weekend_5":  0.057859,
		"Jun_Weekend_6":  0.058619,
		"Jun_Weekend_7":  0.049029,
		"Jun_Weekend_8":  0.041535,
		"Jun_Weekend_9":  0.042520,
		"Mar_Weekday_0":  0.058023,
		"Mar_Weekday_1":  0.054835,
		"Mar_Weekday_10": 0.045125,
		"Mar_Weekday_11": 0.044471,
		"Mar_Weekday_12": 0.043584,
		"Mar_Weekday_13": 0.041100,
		"Mar_Weekday_14": 0.039340,
		"Mar_Weekday_15": 0.040142,
		"Mar_Weekday_16": 0.045600,
		"Mar_Weekday_17": 0.060402,
		"Mar_Weekday_18": 0.071836,
		"Mar_Weekday_19": 0.071684,
		"Mar_Weekday_2":  0.057368,
		"Mar_Weekday_20": 0.068753,
		"Mar_Weekday_21": 0.068491,
		"Mar_Weekday_22": 0.065697,
		"Mar_Weekday_23": 0.058824,
		"Mar_Weekday_3":  0.056450,
		"Mar_Weekday_4":  0.056677,
		"Mar_Weekday_5":  0.056366,
		"Mar_Weekday_6":  0.062776,
		"Mar_Weekday_7":  0.063299,
		"Mar_Weekday_8":  0.055087,
		"Mar_Weekday_9":  0.048087,
		"Mar_Weekend_0":  0.055494,
		"Mar_Weekend_1":  0.056183,
		"Mar_Weekend_10": 0.018085,
		"Mar_Weekend_11": 0.018659,
		"Mar_Weekend_12": 0.016041,
		"Mar_Weekend_13": 0.010959,
		"Mar_Weekend_14": 0.008077,
		"Mar_Weekend_15": 0.009321,
		"Mar_Weekend_16": 0.016148,
		"Mar_Weekend_17": 0.053878,
		"Mar_Weekend_18": 0.077123,
		"Mar_Weekend_19": 0.076887,
		"Mar_Weekend_2":  0.057152,
		"Mar_Weekend_20": 0.076227,
		"Mar_Weekend_21": 0.074830,
		"Mar_Weekend_22": 0.073063,
		"Mar_Weekend_23": 0.066899,
		"Mar_Weekend_3":  0.055304,
		"Mar_Weekend_4":  0.055699,
		"Mar_Weekend_5":  0.052752,
		"Mar_Weekend_6":  0.053092,
		"Mar_Weekend_7":  0.042222,
		"Mar_Weekend_8":  0.021078,
		"Mar_Weekend_9":  0.018681,
		"May_Weekday_0":  0.064448,
		"May_Weekday_1":  0.057157,
		"May_Weekday_10": 0.030332,
		"May_Weekday_11": 0.030219,
		"May_Weekday_12": 0.028248,
		"May_Weekday_13": 0.028592,
		"May_Weekday_14": 0.029078,
		"May_Weekday_15": 0.030203,
		"May_Weekday_16": 0.030763,
		"May_Weekday_17": 0.042920,
		"May_Weekday_18": 0.064735,
		"May_Weekday_19": 0.065943,
		"May_Weekday_2":  0.055938,
		"May_Weekday_20": 0.064335,
		"May_Weekday_21": 0.064617,
		"May_Weekday_22": 0.065687,
		"May_Weekday_23": 0.065272,
		"May_Weekday_3":  0.055742,
		"May_Weekday_4":  0.057207,
		"May_Weekday_5":  0.062095,
		"May_Weekday_6":  0.065808,
		"May_Weekday_7":  0.047268,
		"May_Weekday_8":  0.032599,
		"May_Weekday_9":  0.030164,
		"May_Weekend_0":  0.063919,
		"May_Weekend_1":  0.063025,
		"May_Weekend_10": 0.009260,
		"May_Weekend_11": 0.009272,
		"May_Weekend_12": 0.009227,
		"May_Weekend_13": 0.008643,
		"May_Weekend_14": 0.010100,
		"May_Weekend_15": 0.006262,
		"May_Weekend_16": 0.005062,
		"May_Weekend_17": 0.012107,
		"May_Weekend_18": 0.064576,
		"May_Weekend_19": 0.063043,
		"May_Weekend_2":  0.058268,
		"May_Weekend_20": 0.060905,
		"May_Weekend_21": 0.061000,
		"May_Weekend_22": 0.062872,
		"May_Weekend_23": 0.065872,
		"May_Weekend_3":  0.053017,
		"May_Weekend_4":  0.056340,
		"May_Weekend_5":  0.058775,
		"May_Weekend_6":  0.044630,
		"May_Weekend_7":  0.012385,
		"May_Weekend_8":  0.009419,
		"May_Weekend_9":  0.009362,
		"Nov_Weekday_0":  0.056363,
		"Nov_Weekday_1":  0.056570,
		"Nov_Weekday_10": 0.060416,
		"Nov_Weekday_11": 0.060422,
		"Nov_Weekday_12": 0.060386,
		"Nov_Weekday_13": 0.061386,
		"Nov_Weekday_14": 0.060928,
		"Nov_Weekday_15": 0.065267,
		"Nov_Weekday_16": 0.070120,
		"Nov_Weekday_17": 0.065261,
		"Nov_Weekday_18": 0.063690,
		"Nov_Weekday_19": 0.065090,
		"Nov_Weekday_2":  0.056052,
		"Nov_Weekday_20": 0.066997,
		"Nov_Weekday_21": 0.066442,
		"Nov_Weekday_22": 0.058569,
		"Nov_Weekday_23": 0.057983,
		"Nov_Weekday_3":  0.055925,
		"Nov_Weekday_4":  0.056103,
		"Nov_Weekday_5":  0.056804,
		"Nov_Weekday_6":  0.059953,
		"Nov_Weekday_7":  0.060357,
		"Nov_Weekday_8":  0.061458,
		"Nov_Weekday_9":  0.061894,
		"Nov_Weekend_0":  0.057451,
		"Nov_Weekend_1":  0.056036,
		"Nov_Weekend_10": 0.044266,
		"Nov_Weekend_11": 0.044602,
		"Nov_Weekend_12": 0.044265,
		"Nov_Weekend_13": 0.045029,
		"Nov_Weekend_14": 0.047369,
		"Nov_Weekend_15": 0.052576,
		"Nov_Weekend_16": 0.066965,
		"Nov_Weekend_17": 0.067957,
		"Nov_Weekend_18": 0.065968,
		"Nov_Weekend_19": 0.067452,
		"Nov_Weekend_2":  0.055119,
		"Nov_Weekend_20": 0.067297,
		"Nov_Weekend_21": 0.064626,
		"Nov_Weekend_22": 0.061090,
		"Nov_Weekend_23": 0.060640,
		"Nov_Weekend_3":  0.054358,
		"Nov_Weekend_4":  0.053492,
		"Nov_Weekend_5":  0.053298,
		"Nov_Weekend_6":  0.052738,
		"Nov_Weekend_7":  0.044796,
		"Nov_Weekend_8":  0.043209,
		"Nov_Weekend_9":  0.044204,
		"Oct_Weekday_0":  0.063541,
		"Oct_Weekday_1":  0.058402,
		"Oct_Weekday_10": 0.061254,
		"Oct_Weekday_11": 0.063529,
		"Oct_Weekday_12": 0.062169,
		"Oct_Weekday_13": 0.060935,
		"Oct_Weekday_14": 0.062305,
		"Oct_Weekday_15": 0.308452,
		"Oct_Weekday_16": 0.437279,
		"Oct_Weekday_17": 0.470729,
		"Oct_Weekday_18": 0.475102,
		"Oct_Weekday_19": 0.475918,
		"Oct_Weekday_2":  0.058539,
		"Oct_Weekday_20": 0.112095,
		"Oct_Weekday_21": 0.087672,
		"Oct_Weekday_22": 0.065749,
		"Oct_Weekday_23": 0.060466,
		"Oct_Weekday_3":  0.058368,
		"Oct_Weekday_4":  0.058429,
		"Oct_Weekday_5":  0.058984,
		"Oct_Weekday_6":  0.059641,
		"Oct_Weekday_7":  0.060572,
		"Oct_Weekday_8":  0.059924,
		"Oct_Weekday_9":  0.061260,
		"Oct_Weekend_0":  0.058546,
		"Oct_Weekend_1":  0.058228,
		"Oct_Weekend_10": 0.047665,
		"Oct_Weekend_11": 0.048371,
		"Oct_Weekend_12": 0.049149,
		"Oct_Weekend_13": 0.047023,
		"Oct_Weekend_14": 0.046585,
		"Oct_Weekend_15": 0.048790,
		"Oct_Weekend_16": 0.054123,
		"Oct_Weekend_17": 0.081364,
		"Oct_Weekend_18": 0.073833,
		"Oct_Weekend_19": 0.089401,
		"Oct_Weekend_2":  0.057023,
		"Oct_Weekend_20": 0.070336,
		"Oct_Weekend_21": 0.070891,
		"Oct_Weekend_22": 0.068493,
		"Oct_Weekend_23": 0.064435,
		"Oct_Weekend_3":  0.057473,
		"Oct_Weekend_4":  0.056578,
		"Oct_Weekend_5":  0.056056,
		"Oct_Weekend_6":  0.055614,
		"Oct_Weekend_7":  0.052792,
		"Oct_Weekend_8":  0.047857,
		"Oct_Weekend_9":  0.047301,
		"Sep_Weekday_0":  0.073310,
		"Sep_Weekday_1":  0.058359,
		"Sep_Weekday_10": 0.062469,
		"Sep_Weekday_11": 0.062027,
		"Sep_Weekday_12": 0.062218,
		"Sep_Weekday_13": 0.062661,
		"Sep_Weekday_14": 0.189042,
		"Sep_Weekday_15": 0.325435,
		"Sep_Weekday_16": 0.343111,
		"Sep_Weekday_17": 0.388609,
		"Sep_Weekday_18": 1.925876,
		"Sep_Weekday_19": 2.152776,
		"Sep_Weekday_2":  0.058100,
		"Sep_Weekday_20": 0.288340,
		"Sep_Weekday_21": 0.089622,
		"Sep_Weekday_22": 0.070744,
		"Sep_Weekday_23": 0.064551,
		"Sep_Weekday_3":  0.057933,
		"Sep_Weekday_4":  0.057601,
		"Sep_Weekday_5":  0.058025,
		"Sep_Weekday_6":  0.059591,
		"Sep_Weekday_7":  0.061186,
		"Sep_Weekday_8":  0.059056,
		"Sep_Weekday_9":  0.064430,
		"Sep_Weekend_0":  0.062861,
		"Sep_Weekend_1":  0.059378,
		"Sep_Weekend_10": 0.054691,
		"Sep_Weekend_11": 0.053824,
		"Sep_Weekend_12": 0.052309,
		"Sep_Weekend_13": 0.050564,
		"Sep_Weekend_14": 0.049901,
		"Sep_Weekend_15": 0.051247,
		"Sep_Weekend_16": 0.352989,
		"Sep_Weekend_17": 0.376611,
		"Sep_Weekend_18": 1.759964,
		"Sep_Weekend_19": 1.980136,
		"Sep_Weekend_2":  0.058549,
		"Sep_Weekend_20": 0.181285,
		"Sep_Weekend_21": 0.093194,
		"Sep_Weekend_22": 0.072380,
		"Sep_Weekend_23": 0.065917,
		"Sep_Weekend_3":  0.058278,
		"Sep_Weekend_4":  0.057751,
		"Sep_Weekend_5":  0.057635,
		"Sep_Weekend_6":  0.056816,
		"Sep_Weekend_7":  0.052325,
		"Sep_Weekend_8":  0.049936,
		"Sep_Weekend_9":  0.052512,
	},
}

// sdgeNBTData contains Avoided Cost Calculator (ACC) NBT25 and NBT26 export rates ($/kWh)
// (and 2026 NBT00 non-locked rates) for 2026 and 2027, representing the sum of Generation
// (USCA-XXSD) and Delivery (USCA-SDXX) credit rates, grouped by Month_DayType_Hour.
var sdgeNBTData = map[int]map[string]float64{
	2026: {
		"Apr_Weekday_0":  0.073861,
		"Apr_Weekday_1":  0.071755,
		"Apr_Weekday_10": 0.007929,
		"Apr_Weekday_11": 0.007010,
		"Apr_Weekday_12": 0.004092,
		"Apr_Weekday_13": 0.001456,
		"Apr_Weekday_14": 0.000604,
		"Apr_Weekday_15": 0.000125,
		"Apr_Weekday_16": 0.000763,
		"Apr_Weekday_17": 0.004608,
		"Apr_Weekday_18": 0.073313,
		"Apr_Weekday_19": 0.073093,
		"Apr_Weekday_2":  0.076762,
		"Apr_Weekday_20": 0.072325,
		"Apr_Weekday_21": 0.070852,
		"Apr_Weekday_22": 0.068243,
		"Apr_Weekday_23": 0.074473,
		"Apr_Weekday_3":  0.074343,
		"Apr_Weekday_4":  0.073300,
		"Apr_Weekday_5":  0.076342,
		"Apr_Weekday_6":  0.067844,
		"Apr_Weekday_7":  0.054572,
		"Apr_Weekday_8":  0.012343,
		"Apr_Weekday_9":  0.004790,
		"Apr_Weekend_0":  0.064273,
		"Apr_Weekend_1":  0.064986,
		"Apr_Weekend_10": 0.007473,
		"Apr_Weekend_11": 0.004414,
		"Apr_Weekend_12": 0.000726,
		"Apr_Weekend_13": 0.000000,
		"Apr_Weekend_14": 0.000000,
		"Apr_Weekend_15": 0.000000,
		"Apr_Weekend_16": 0.000000,
		"Apr_Weekend_17": 0.002651,
		"Apr_Weekend_18": 0.068347,
		"Apr_Weekend_19": 0.078140,
		"Apr_Weekend_2":  0.072086,
		"Apr_Weekend_20": 0.066336,
		"Apr_Weekend_21": 0.067226,
		"Apr_Weekend_22": 0.065858,
		"Apr_Weekend_23": 0.067981,
		"Apr_Weekend_3":  0.076292,
		"Apr_Weekend_4":  0.072639,
		"Apr_Weekend_5":  0.073763,
		"Apr_Weekend_6":  0.067064,
		"Apr_Weekend_7":  0.044901,
		"Apr_Weekend_8":  0.001537,
		"Apr_Weekend_9":  0.001788,
		"Aug_Weekday_0":  0.087316,
		"Aug_Weekday_1":  0.088314,
		"Aug_Weekday_10": 0.061365,
		"Aug_Weekday_11": 0.060787,
		"Aug_Weekday_12": 0.060721,
		"Aug_Weekday_13": 0.060483,
		"Aug_Weekday_14": 0.060759,
		"Aug_Weekday_15": 0.064161,
		"Aug_Weekday_16": 0.077103,
		"Aug_Weekday_17": 0.924946,
		"Aug_Weekday_18": 1.069771,
		"Aug_Weekday_19": 0.908028,
		"Aug_Weekday_2":  0.085097,
		"Aug_Weekday_20": 0.984815,
		"Aug_Weekday_21": 0.877307,
		"Aug_Weekday_22": 0.865975,
		"Aug_Weekday_23": 0.090043,
		"Aug_Weekday_3":  0.081429,
		"Aug_Weekday_4":  0.081097,
		"Aug_Weekday_5":  0.081522,
		"Aug_Weekday_6":  0.077987,
		"Aug_Weekday_7":  0.069241,
		"Aug_Weekday_8":  0.062737,
		"Aug_Weekday_9":  0.061610,
		"Aug_Weekend_0":  0.086310,
		"Aug_Weekend_1":  0.084451,
		"Aug_Weekend_10": 0.038209,
		"Aug_Weekend_11": 0.038301,
		"Aug_Weekend_12": 0.035091,
		"Aug_Weekend_13": 0.034742,
		"Aug_Weekend_14": 0.035321,
		"Aug_Weekend_15": 0.041747,
		"Aug_Weekend_16": 0.056618,
		"Aug_Weekend_17": 0.868307,
		"Aug_Weekend_18": 0.922775,
		"Aug_Weekend_19": 0.945792,
		"Aug_Weekend_2":  0.086012,
		"Aug_Weekend_20": 1.019649,
		"Aug_Weekend_21": 0.932945,
		"Aug_Weekend_22": 0.924969,
		"Aug_Weekend_23": 0.088768,
		"Aug_Weekend_3":  0.079428,
		"Aug_Weekend_4":  0.076436,
		"Aug_Weekend_5":  0.076520,
		"Aug_Weekend_6":  0.070688,
		"Aug_Weekend_7":  0.059786,
		"Aug_Weekend_8":  0.042129,
		"Aug_Weekend_9":  0.038652,
		"Dec_Weekday_0":  0.085735,
		"Dec_Weekday_1":  0.083980,
		"Dec_Weekday_10": 0.064592,
		"Dec_Weekday_11": 0.060972,
		"Dec_Weekday_12": 0.057166,
		"Dec_Weekday_13": 0.057568,
		"Dec_Weekday_14": 0.056678,
		"Dec_Weekday_15": 0.067270,
		"Dec_Weekday_16": 0.091712,
		"Dec_Weekday_17": 0.096525,
		"Dec_Weekday_18": 0.095787,
		"Dec_Weekday_19": 0.088786,
		"Dec_Weekday_2":  0.080860,
		"Dec_Weekday_20": 0.087545,
		"Dec_Weekday_21": 0.087134,
		"Dec_Weekday_22": 0.088656,
		"Dec_Weekday_23": 0.088935,
		"Dec_Weekday_3":  0.081487,
		"Dec_Weekday_4":  0.083587,
		"Dec_Weekday_5":  0.086129,
		"Dec_Weekday_6":  0.095979,
		"Dec_Weekday_7":  0.080161,
		"Dec_Weekday_8":  0.071046,
		"Dec_Weekday_9":  0.066888,
		"Dec_Weekend_0":  0.079651,
		"Dec_Weekend_1":  0.078065,
		"Dec_Weekend_10": 0.020983,
		"Dec_Weekend_11": 0.019103,
		"Dec_Weekend_12": 0.020330,
		"Dec_Weekend_13": 0.014622,
		"Dec_Weekend_14": 0.014007,
		"Dec_Weekend_15": 0.040505,
		"Dec_Weekend_16": 0.081130,
		"Dec_Weekend_17": 0.086330,
		"Dec_Weekend_18": 0.084193,
		"Dec_Weekend_19": 0.080087,
		"Dec_Weekend_2":  0.074512,
		"Dec_Weekend_20": 0.078888,
		"Dec_Weekend_21": 0.074242,
		"Dec_Weekend_22": 0.075531,
		"Dec_Weekend_23": 0.074494,
		"Dec_Weekend_3":  0.070760,
		"Dec_Weekend_4":  0.069793,
		"Dec_Weekend_5":  0.071724,
		"Dec_Weekend_6":  0.074763,
		"Dec_Weekend_7":  0.064863,
		"Dec_Weekend_8":  0.042413,
		"Dec_Weekend_9":  0.022129,
		"Feb_Weekday_0":  0.088028,
		"Feb_Weekday_1":  0.085488,
		"Feb_Weekday_10": 0.049321,
		"Feb_Weekday_11": 0.047404,
		"Feb_Weekday_12": 0.044042,
		"Feb_Weekday_13": 0.039312,
		"Feb_Weekday_14": 0.032813,
		"Feb_Weekday_15": 0.030944,
		"Feb_Weekday_16": 0.064678,
		"Feb_Weekday_17": 0.107243,
		"Feb_Weekday_18": 0.091985,
		"Feb_Weekday_19": 0.086285,
		"Feb_Weekday_2":  0.087321,
		"Feb_Weekday_20": 0.086716,
		"Feb_Weekday_21": 0.083827,
		"Feb_Weekday_22": 0.090950,
		"Feb_Weekday_23": 0.087167,
		"Feb_Weekday_3":  0.087463,
		"Feb_Weekday_4":  0.089466,
		"Feb_Weekday_5":  0.087894,
		"Feb_Weekday_6":  0.084338,
		"Feb_Weekday_7":  0.081160,
		"Feb_Weekday_8":  0.063054,
		"Feb_Weekday_9":  0.052098,
		"Feb_Weekend_0":  0.084484,
		"Feb_Weekend_1":  0.085515,
		"Feb_Weekend_10": 0.026981,
		"Feb_Weekend_11": 0.024705,
		"Feb_Weekend_12": 0.024380,
		"Feb_Weekend_13": 0.022058,
		"Feb_Weekend_14": 0.021632,
		"Feb_Weekend_15": 0.018969,
		"Feb_Weekend_16": 0.056905,
		"Feb_Weekend_17": 0.097805,
		"Feb_Weekend_18": 0.100540,
		"Feb_Weekend_19": 0.088926,
		"Feb_Weekend_2":  0.084161,
		"Feb_Weekend_20": 0.085948,
		"Feb_Weekend_21": 0.082901,
		"Feb_Weekend_22": 0.083873,
		"Feb_Weekend_23": 0.090545,
		"Feb_Weekend_3":  0.084004,
		"Feb_Weekend_4":  0.083048,
		"Feb_Weekend_5":  0.082858,
		"Feb_Weekend_6":  0.079159,
		"Feb_Weekend_7":  0.069955,
		"Feb_Weekend_8":  0.047765,
		"Feb_Weekend_9":  0.027013,
		"Jan_Weekday_0":  0.088830,
		"Jan_Weekday_1":  0.086708,
		"Jan_Weekday_10": 0.063699,
		"Jan_Weekday_11": 0.062991,
		"Jan_Weekday_12": 0.062930,
		"Jan_Weekday_13": 0.060225,
		"Jan_Weekday_14": 0.059334,
		"Jan_Weekday_15": 0.061243,
		"Jan_Weekday_16": 0.078078,
		"Jan_Weekday_17": 0.108968,
		"Jan_Weekday_18": 0.099709,
		"Jan_Weekday_19": 0.090204,
		"Jan_Weekday_2":  0.084462,
		"Jan_Weekday_20": 0.089121,
		"Jan_Weekday_21": 0.089736,
		"Jan_Weekday_22": 0.093450,
		"Jan_Weekday_23": 0.091064,
		"Jan_Weekday_3":  0.083976,
		"Jan_Weekday_4":  0.086038,
		"Jan_Weekday_5":  0.089678,
		"Jan_Weekday_6":  0.089931,
		"Jan_Weekday_7":  0.085156,
		"Jan_Weekday_8":  0.073250,
		"Jan_Weekday_9":  0.065376,
		"Jan_Weekend_0":  0.087919,
		"Jan_Weekend_1":  0.088519,
		"Jan_Weekend_10": 0.054304,
		"Jan_Weekend_11": 0.053036,
		"Jan_Weekend_12": 0.053382,
		"Jan_Weekend_13": 0.048367,
		"Jan_Weekend_14": 0.052361,
		"Jan_Weekend_15": 0.054240,
		"Jan_Weekend_16": 0.079013,
		"Jan_Weekend_17": 0.104224,
		"Jan_Weekend_18": 0.093809,
		"Jan_Weekend_19": 0.086724,
		"Jan_Weekend_2":  0.086612,
		"Jan_Weekend_20": 0.085628,
		"Jan_Weekend_21": 0.084854,
		"Jan_Weekend_22": 0.088293,
		"Jan_Weekend_23": 0.085040,
		"Jan_Weekend_3":  0.085371,
		"Jan_Weekend_4":  0.084260,
		"Jan_Weekend_5":  0.085358,
		"Jan_Weekend_6":  0.087952,
		"Jan_Weekend_7":  0.079567,
		"Jan_Weekend_8":  0.065637,
		"Jan_Weekend_9":  0.055888,
		"Jul_Weekday_0":  0.072294,
		"Jul_Weekday_1":  0.072987,
		"Jul_Weekday_10": 0.058309,
		"Jul_Weekday_11": 0.055999,
		"Jul_Weekday_12": 0.051838,
		"Jul_Weekday_13": 0.047670,
		"Jul_Weekday_14": 0.047448,
		"Jul_Weekday_15": 0.049673,
		"Jul_Weekday_16": 0.153664,
		"Jul_Weekday_17": 0.278933,
		"Jul_Weekday_18": 0.323917,
		"Jul_Weekday_19": 0.332083,
		"Jul_Weekday_2":  0.070073,
		"Jul_Weekday_20": 0.205029,
		"Jul_Weekday_21": 0.084826,
		"Jul_Weekday_22": 0.081156,
		"Jul_Weekday_23": 0.074730,
		"Jul_Weekday_3":  0.070674,
		"Jul_Weekday_4":  0.067637,
		"Jul_Weekday_5":  0.067539,
		"Jul_Weekday_6":  0.070530,
		"Jul_Weekday_7":  0.067173,
		"Jul_Weekday_8":  0.063947,
		"Jul_Weekday_9":  0.057203,
		"Jul_Weekend_0":  0.079276,
		"Jul_Weekend_1":  0.072554,
		"Jul_Weekend_10": 0.026333,
		"Jul_Weekend_11": 0.022128,
		"Jul_Weekend_12": 0.024542,
		"Jul_Weekend_13": 0.025633,
		"Jul_Weekend_14": 0.019821,
		"Jul_Weekend_15": 0.023676,
		"Jul_Weekend_16": 0.023705,
		"Jul_Weekend_17": 0.043261,
		"Jul_Weekend_18": 0.096132,
		"Jul_Weekend_19": 0.118209,
		"Jul_Weekend_2":  0.070130,
		"Jul_Weekend_20": 0.102810,
		"Jul_Weekend_21": 0.081103,
		"Jul_Weekend_22": 0.078378,
		"Jul_Weekend_23": 0.075014,
		"Jul_Weekend_3":  0.069742,
		"Jul_Weekend_4":  0.067084,
		"Jul_Weekend_5":  0.067810,
		"Jul_Weekend_6":  0.064218,
		"Jul_Weekend_7":  0.061319,
		"Jul_Weekend_8":  0.024559,
		"Jul_Weekend_9":  0.024347,
		"Jun_Weekday_0":  0.075110,
		"Jun_Weekday_1":  0.072921,
		"Jun_Weekday_10": 0.033042,
		"Jun_Weekday_11": 0.028508,
		"Jun_Weekday_12": 0.028145,
		"Jun_Weekday_13": 0.029040,
		"Jun_Weekday_14": 0.033089,
		"Jun_Weekday_15": 0.027507,
		"Jun_Weekday_16": 0.030903,
		"Jun_Weekday_17": 0.043873,
		"Jun_Weekday_18": 0.086613,
		"Jun_Weekday_19": 0.089804,
		"Jun_Weekday_2":  0.069859,
		"Jun_Weekday_20": 0.084913,
		"Jun_Weekday_21": 0.080929,
		"Jun_Weekday_22": 0.076009,
		"Jun_Weekday_23": 0.069973,
		"Jun_Weekday_3":  0.073075,
		"Jun_Weekday_4":  0.070771,
		"Jun_Weekday_5":  0.069764,
		"Jun_Weekday_6":  0.065914,
		"Jun_Weekday_7":  0.060186,
		"Jun_Weekday_8":  0.041789,
		"Jun_Weekday_9":  0.035090,
		"Jun_Weekend_0":  0.071000,
		"Jun_Weekend_1":  0.068935,
		"Jun_Weekend_10": 0.017526,
		"Jun_Weekend_11": 0.011107,
		"Jun_Weekend_12": 0.012111,
		"Jun_Weekend_13": 0.006866,
		"Jun_Weekend_14": 0.008648,
		"Jun_Weekend_15": 0.008815,
		"Jun_Weekend_16": 0.009685,
		"Jun_Weekend_17": 0.020969,
		"Jun_Weekend_18": 0.069947,
		"Jun_Weekend_19": 0.093212,
		"Jun_Weekend_2":  0.068948,
		"Jun_Weekend_20": 0.080719,
		"Jun_Weekend_21": 0.076594,
		"Jun_Weekend_22": 0.075913,
		"Jun_Weekend_23": 0.071979,
		"Jun_Weekend_3":  0.069319,
		"Jun_Weekend_4":  0.066550,
		"Jun_Weekend_5":  0.082705,
		"Jun_Weekend_6":  0.068808,
		"Jun_Weekend_7":  0.056840,
		"Jun_Weekend_8":  0.017600,
		"Jun_Weekend_9":  0.013036,
		"Mar_Weekday_0":  0.073665,
		"Mar_Weekday_1":  0.070120,
		"Mar_Weekday_10": 0.017085,
		"Mar_Weekday_11": 0.015902,
		"Mar_Weekday_12": 0.016159,
		"Mar_Weekday_13": 0.012092,
		"Mar_Weekday_14": 0.010517,
		"Mar_Weekday_15": 0.011646,
		"Mar_Weekday_16": 0.022663,
		"Mar_Weekday_17": 0.056232,
		"Mar_Weekday_18": 0.080976,
		"Mar_Weekday_19": 0.079902,
		"Mar_Weekday_2":  0.066212,
		"Mar_Weekday_20": 0.074966,
		"Mar_Weekday_21": 0.072088,
		"Mar_Weekday_22": 0.068068,
		"Mar_Weekday_23": 0.069681,
		"Mar_Weekday_3":  0.065625,
		"Mar_Weekday_4":  0.066786,
		"Mar_Weekday_5":  0.069908,
		"Mar_Weekday_6":  0.072157,
		"Mar_Weekday_7":  0.065179,
		"Mar_Weekday_8":  0.046375,
		"Mar_Weekday_9":  0.021641,
		"Mar_Weekend_0":  0.066816,
		"Mar_Weekend_1":  0.068071,
		"Mar_Weekend_10": 0.004610,
		"Mar_Weekend_11": 0.002852,
		"Mar_Weekend_12": 0.000613,
		"Mar_Weekend_13": 0.000599,
		"Mar_Weekend_14": 0.000000,
		"Mar_Weekend_15": 0.003786,
		"Mar_Weekend_16": 0.016353,
		"Mar_Weekend_17": 0.031412,
		"Mar_Weekend_18": 0.070648,
		"Mar_Weekend_19": 0.073238,
		"Mar_Weekend_2":  0.069400,
		"Mar_Weekend_20": 0.070843,
		"Mar_Weekend_21": 0.070378,
		"Mar_Weekend_22": 0.068560,
		"Mar_Weekend_23": 0.064831,
		"Mar_Weekend_3":  0.070948,
		"Mar_Weekend_4":  0.069934,
		"Mar_Weekend_5":  0.069895,
		"Mar_Weekend_6":  0.067394,
		"Mar_Weekend_7":  0.060898,
		"Mar_Weekend_8":  0.006312,
		"Mar_Weekend_9":  0.001821,
		"May_Weekday_0":  0.076833,
		"May_Weekday_1":  0.072788,
		"May_Weekday_10": 0.013070,
		"May_Weekday_11": 0.009893,
		"May_Weekday_12": 0.009405,
		"May_Weekday_13": 0.010401,
		"May_Weekday_14": 0.008831,
		"May_Weekday_15": 0.008420,
		"May_Weekday_16": 0.011375,
		"May_Weekday_17": 0.031897,
		"May_Weekday_18": 0.080047,
		"May_Weekday_19": 0.085340,
		"May_Weekday_2":  0.075276,
		"May_Weekday_20": 0.074454,
		"May_Weekday_21": 0.072879,
		"May_Weekday_22": 0.074252,
		"May_Weekday_23": 0.075341,
		"May_Weekday_3":  0.079648,
		"May_Weekday_4":  0.080054,
		"May_Weekday_5":  0.073664,
		"May_Weekday_6":  0.065571,
		"May_Weekday_7":  0.061636,
		"May_Weekday_8":  0.022543,
		"May_Weekday_9":  0.011919,
		"May_Weekend_0":  0.068009,
		"May_Weekend_1":  0.069648,
		"May_Weekend_10": 0.004248,
		"May_Weekend_11": 0.004956,
		"May_Weekend_12": 0.004478,
		"May_Weekend_13": 0.003822,
		"May_Weekend_14": 0.000000,
		"May_Weekend_15": 0.000000,
		"May_Weekend_16": 0.000000,
		"May_Weekend_17": 0.000636,
		"May_Weekend_18": 0.056846,
		"May_Weekend_19": 0.083510,
		"May_Weekend_2":  0.079519,
		"May_Weekend_20": 0.076365,
		"May_Weekend_21": 0.074192,
		"May_Weekend_22": 0.074068,
		"May_Weekend_23": 0.075526,
		"May_Weekend_3":  0.080790,
		"May_Weekend_4":  0.075809,
		"May_Weekend_5":  0.069268,
		"May_Weekend_6":  0.067283,
		"May_Weekend_7":  0.031926,
		"May_Weekend_8":  0.001904,
		"May_Weekend_9":  0.004496,
		"Nov_Weekday_0":  0.083465,
		"Nov_Weekday_1":  0.082191,
		"Nov_Weekday_10": 0.057273,
		"Nov_Weekday_11": 0.054974,
		"Nov_Weekday_12": 0.053593,
		"Nov_Weekday_13": 0.047525,
		"Nov_Weekday_14": 0.048911,
		"Nov_Weekday_15": 0.063249,
		"Nov_Weekday_16": 0.090045,
		"Nov_Weekday_17": 0.092403,
		"Nov_Weekday_18": 0.086679,
		"Nov_Weekday_19": 0.084647,
		"Nov_Weekday_2":  0.080582,
		"Nov_Weekday_20": 0.083644,
		"Nov_Weekday_21": 0.085518,
		"Nov_Weekday_22": 0.086594,
		"Nov_Weekday_23": 0.089476,
		"Nov_Weekday_3":  0.079505,
		"Nov_Weekday_4":  0.083618,
		"Nov_Weekday_5":  0.085306,
		"Nov_Weekday_6":  0.081837,
		"Nov_Weekday_7":  0.072001,
		"Nov_Weekday_8":  0.062130,
		"Nov_Weekday_9":  0.059489,
		"Nov_Weekend_0":  0.079979,
		"Nov_Weekend_1":  0.076610,
		"Nov_Weekend_10": 0.024142,
		"Nov_Weekend_11": 0.021353,
		"Nov_Weekend_12": 0.020947,
		"Nov_Weekend_13": 0.020477,
		"Nov_Weekend_14": 0.021288,
		"Nov_Weekend_15": 0.039849,
		"Nov_Weekend_16": 0.077317,
		"Nov_Weekend_17": 0.088893,
		"Nov_Weekend_18": 0.084457,
		"Nov_Weekend_19": 0.080977,
		"Nov_Weekend_2":  0.075178,
		"Nov_Weekend_20": 0.078812,
		"Nov_Weekend_21": 0.078353,
		"Nov_Weekend_22": 0.082666,
		"Nov_Weekend_23": 0.078340,
		"Nov_Weekend_3":  0.074260,
		"Nov_Weekend_4":  0.074994,
		"Nov_Weekend_5":  0.076326,
		"Nov_Weekend_6":  0.076349,
		"Nov_Weekend_7":  0.056052,
		"Nov_Weekend_8":  0.035856,
		"Nov_Weekend_9":  0.026572,
		"Oct_Weekday_0":  0.086108,
		"Oct_Weekday_1":  0.086699,
		"Oct_Weekday_10": 0.056688,
		"Oct_Weekday_11": 0.054945,
		"Oct_Weekday_12": 0.054259,
		"Oct_Weekday_13": 0.053647,
		"Oct_Weekday_14": 0.053285,
		"Oct_Weekday_15": 0.053008,
		"Oct_Weekday_16": 0.058757,
		"Oct_Weekday_17": 0.186990,
		"Oct_Weekday_18": 0.192285,
		"Oct_Weekday_19": 0.090855,
		"Oct_Weekday_2":  0.083384,
		"Oct_Weekday_20": 0.087074,
		"Oct_Weekday_21": 0.081269,
		"Oct_Weekday_22": 0.087816,
		"Oct_Weekday_23": 0.088687,
		"Oct_Weekday_3":  0.079285,
		"Oct_Weekday_4":  0.079258,
		"Oct_Weekday_5":  0.079296,
		"Oct_Weekday_6":  0.083516,
		"Oct_Weekday_7":  0.073522,
		"Oct_Weekday_8":  0.063544,
		"Oct_Weekday_9":  0.057829,
		"Oct_Weekend_0":  0.078635,
		"Oct_Weekend_1":  0.079561,
		"Oct_Weekend_10": 0.021034,
		"Oct_Weekend_11": 0.020321,
		"Oct_Weekend_12": 0.019963,
		"Oct_Weekend_13": 0.019072,
		"Oct_Weekend_14": 0.020612,
		"Oct_Weekend_15": 0.021686,
		"Oct_Weekend_16": 0.037766,
		"Oct_Weekend_17": 0.079146,
		"Oct_Weekend_18": 0.083440,
		"Oct_Weekend_19": 0.081874,
		"Oct_Weekend_2":  0.076111,
		"Oct_Weekend_20": 0.079025,
		"Oct_Weekend_21": 0.076746,
		"Oct_Weekend_22": 0.088594,
		"Oct_Weekend_23": 0.082960,
		"Oct_Weekend_3":  0.076768,
		"Oct_Weekend_4":  0.075186,
		"Oct_Weekend_5":  0.074401,
		"Oct_Weekend_6":  0.072063,
		"Oct_Weekend_7":  0.064841,
		"Oct_Weekend_8":  0.043182,
		"Oct_Weekend_9":  0.021050,
		"Sep_Weekday_0":  0.089501,
		"Sep_Weekday_1":  0.087382,
		"Sep_Weekday_10": 0.055088,
		"Sep_Weekday_11": 0.054825,
		"Sep_Weekday_12": 0.054642,
		"Sep_Weekday_13": 0.056093,
		"Sep_Weekday_14": 0.053518,
		"Sep_Weekday_15": 0.051662,
		"Sep_Weekday_16": 0.068327,
		"Sep_Weekday_17": 0.131897,
		"Sep_Weekday_18": 0.450081,
		"Sep_Weekday_19": 0.581786,
		"Sep_Weekday_2":  0.085859,
		"Sep_Weekday_20": 0.310407,
		"Sep_Weekday_21": 0.150702,
		"Sep_Weekday_22": 0.150154,
		"Sep_Weekday_23": 0.095243,
		"Sep_Weekday_3":  0.082266,
		"Sep_Weekday_4":  0.081066,
		"Sep_Weekday_5":  0.083890,
		"Sep_Weekday_6":  0.081363,
		"Sep_Weekday_7":  0.068222,
		"Sep_Weekday_8":  0.061030,
		"Sep_Weekday_9":  0.057084,
		"Sep_Weekend_0":  0.079133,
		"Sep_Weekend_1":  0.082018,
		"Sep_Weekend_10": 0.020315,
		"Sep_Weekend_11": 0.019505,
		"Sep_Weekend_12": 0.016615,
		"Sep_Weekend_13": 0.018622,
		"Sep_Weekend_14": 0.022799,
		"Sep_Weekend_15": 0.025479,
		"Sep_Weekend_16": 0.239859,
		"Sep_Weekend_17": 0.534619,
		"Sep_Weekend_18": 0.932390,
		"Sep_Weekend_19": 0.886458,
		"Sep_Weekend_2":  0.080263,
		"Sep_Weekend_20": 0.540889,
		"Sep_Weekend_21": 0.162697,
		"Sep_Weekend_22": 0.163186,
		"Sep_Weekend_23": 0.092353,
		"Sep_Weekend_3":  0.076462,
		"Sep_Weekend_4":  0.075522,
		"Sep_Weekend_5":  0.075457,
		"Sep_Weekend_6":  0.073370,
		"Sep_Weekend_7":  0.060450,
		"Sep_Weekend_8":  0.035832,
		"Sep_Weekend_9":  0.023338,
	},
	2027: {
		"Apr_Weekday_0":  0.079782,
		"Apr_Weekday_1":  0.080519,
		"Apr_Weekday_10": 0.007349,
		"Apr_Weekday_11": 0.009249,
		"Apr_Weekday_12": 0.005174,
		"Apr_Weekday_13": 0.002840,
		"Apr_Weekday_14": 0.002733,
		"Apr_Weekday_15": 0.001289,
		"Apr_Weekday_16": 0.000128,
		"Apr_Weekday_17": 0.004382,
		"Apr_Weekday_18": 0.080913,
		"Apr_Weekday_19": 0.078658,
		"Apr_Weekday_2":  0.086283,
		"Apr_Weekday_20": 0.068717,
		"Apr_Weekday_21": 0.068386,
		"Apr_Weekday_22": 0.069878,
		"Apr_Weekday_23": 0.072680,
		"Apr_Weekday_3":  0.087399,
		"Apr_Weekday_4":  0.084311,
		"Apr_Weekday_5":  0.077797,
		"Apr_Weekday_6":  0.072724,
		"Apr_Weekday_7":  0.057052,
		"Apr_Weekday_8":  0.011624,
		"Apr_Weekday_9":  0.008536,
		"Apr_Weekend_0":  0.061923,
		"Apr_Weekend_1":  0.063890,
		"Apr_Weekend_10": 0.006188,
		"Apr_Weekend_11": 0.002649,
		"Apr_Weekend_12": 0.000964,
		"Apr_Weekend_13": 0.000000,
		"Apr_Weekend_14": 0.000000,
		"Apr_Weekend_15": 0.000000,
		"Apr_Weekend_16": 0.000000,
		"Apr_Weekend_17": 0.000213,
		"Apr_Weekend_18": 0.073208,
		"Apr_Weekend_19": 0.074397,
		"Apr_Weekend_2":  0.079626,
		"Apr_Weekend_20": 0.066025,
		"Apr_Weekend_21": 0.065905,
		"Apr_Weekend_22": 0.068067,
		"Apr_Weekend_23": 0.068343,
		"Apr_Weekend_3":  0.081663,
		"Apr_Weekend_4":  0.082005,
		"Apr_Weekend_5":  0.081415,
		"Apr_Weekend_6":  0.070438,
		"Apr_Weekend_7":  0.041626,
		"Apr_Weekend_8":  0.005204,
		"Apr_Weekend_9":  0.003985,
		"Aug_Weekday_0":  0.109989,
		"Aug_Weekday_1":  0.092451,
		"Aug_Weekday_10": 0.061127,
		"Aug_Weekday_11": 0.061002,
		"Aug_Weekday_12": 0.059583,
		"Aug_Weekday_13": 0.061337,
		"Aug_Weekday_14": 0.060987,
		"Aug_Weekday_15": 0.066872,
		"Aug_Weekday_16": 0.090444,
		"Aug_Weekday_17": 0.975819,
		"Aug_Weekday_18": 1.215630,
		"Aug_Weekday_19": 1.056594,
		"Aug_Weekday_2":  0.090071,
		"Aug_Weekday_20": 1.134175,
		"Aug_Weekday_21": 1.032740,
		"Aug_Weekday_22": 1.019275,
		"Aug_Weekday_23": 0.117699,
		"Aug_Weekday_3":  0.087487,
		"Aug_Weekday_4":  0.085600,
		"Aug_Weekday_5":  0.087679,
		"Aug_Weekday_6":  0.084875,
		"Aug_Weekday_7":  0.072708,
		"Aug_Weekday_8":  0.064324,
		"Aug_Weekday_9":  0.063141,
		"Aug_Weekend_0":  0.110483,
		"Aug_Weekend_1":  0.088533,
		"Aug_Weekend_10": 0.035272,
		"Aug_Weekend_11": 0.031877,
		"Aug_Weekend_12": 0.032158,
		"Aug_Weekend_13": 0.028302,
		"Aug_Weekend_14": 0.035222,
		"Aug_Weekend_15": 0.037012,
		"Aug_Weekend_16": 0.063592,
		"Aug_Weekend_17": 0.913541,
		"Aug_Weekend_18": 1.075648,
		"Aug_Weekend_19": 1.102545,
		"Aug_Weekend_2":  0.088897,
		"Aug_Weekend_20": 1.179883,
		"Aug_Weekend_21": 1.098726,
		"Aug_Weekend_22": 1.090476,
		"Aug_Weekend_23": 0.113526,
		"Aug_Weekend_3":  0.082989,
		"Aug_Weekend_4":  0.080202,
		"Aug_Weekend_5":  0.079247,
		"Aug_Weekend_6":  0.074864,
		"Aug_Weekend_7":  0.060414,
		"Aug_Weekend_8":  0.038483,
		"Aug_Weekend_9":  0.029866,
		"Dec_Weekday_0":  0.090050,
		"Dec_Weekday_1":  0.085827,
		"Dec_Weekday_10": 0.065313,
		"Dec_Weekday_11": 0.063203,
		"Dec_Weekday_12": 0.058127,
		"Dec_Weekday_13": 0.058932,
		"Dec_Weekday_14": 0.055623,
		"Dec_Weekday_15": 0.067392,
		"Dec_Weekday_16": 0.096061,
		"Dec_Weekday_17": 0.097090,
		"Dec_Weekday_18": 0.091983,
		"Dec_Weekday_19": 0.088540,
		"Dec_Weekday_2":  0.082788,
		"Dec_Weekday_20": 0.089239,
		"Dec_Weekday_21": 0.091666,
		"Dec_Weekday_22": 0.093985,
		"Dec_Weekday_23": 0.092112,
		"Dec_Weekday_3":  0.082416,
		"Dec_Weekday_4":  0.084254,
		"Dec_Weekday_5":  0.089711,
		"Dec_Weekday_6":  0.094858,
		"Dec_Weekday_7":  0.082459,
		"Dec_Weekday_8":  0.075595,
		"Dec_Weekday_9":  0.067370,
		"Dec_Weekend_0":  0.085483,
		"Dec_Weekend_1":  0.082128,
		"Dec_Weekend_10": 0.019574,
		"Dec_Weekend_11": 0.017798,
		"Dec_Weekend_12": 0.016288,
		"Dec_Weekend_13": 0.014214,
		"Dec_Weekend_14": 0.014507,
		"Dec_Weekend_15": 0.039937,
		"Dec_Weekend_16": 0.086454,
		"Dec_Weekend_17": 0.093571,
		"Dec_Weekend_18": 0.089962,
		"Dec_Weekend_19": 0.083573,
		"Dec_Weekend_2":  0.079836,
		"Dec_Weekend_20": 0.081214,
		"Dec_Weekend_21": 0.081037,
		"Dec_Weekend_22": 0.083034,
		"Dec_Weekend_23": 0.082220,
		"Dec_Weekend_3":  0.076212,
		"Dec_Weekend_4":  0.075485,
		"Dec_Weekend_5":  0.077933,
		"Dec_Weekend_6":  0.080606,
		"Dec_Weekend_7":  0.066725,
		"Dec_Weekend_8":  0.037864,
		"Dec_Weekend_9":  0.017476,
		"Feb_Weekday_0":  0.091619,
		"Feb_Weekday_1":  0.090513,
		"Feb_Weekday_10": 0.039762,
		"Feb_Weekday_11": 0.037770,
		"Feb_Weekday_12": 0.034521,
		"Feb_Weekday_13": 0.032933,
		"Feb_Weekday_14": 0.027125,
		"Feb_Weekday_15": 0.030265,
		"Feb_Weekday_16": 0.068004,
		"Feb_Weekday_17": 0.115609,
		"Feb_Weekday_18": 0.096925,
		"Feb_Weekday_19": 0.089861,
		"Feb_Weekday_2":  0.091891,
		"Feb_Weekday_20": 0.090659,
		"Feb_Weekday_21": 0.087204,
		"Feb_Weekday_22": 0.095344,
		"Feb_Weekday_23": 0.092189,
		"Feb_Weekday_3":  0.093447,
		"Feb_Weekday_4":  0.093138,
		"Feb_Weekday_5":  0.093069,
		"Feb_Weekday_6":  0.088668,
		"Feb_Weekday_7":  0.084802,
		"Feb_Weekday_8":  0.064691,
		"Feb_Weekday_9":  0.043778,
		"Feb_Weekend_0":  0.086476,
		"Feb_Weekend_1":  0.087897,
		"Feb_Weekend_10": 0.030318,
		"Feb_Weekend_11": 0.026179,
		"Feb_Weekend_12": 0.020724,
		"Feb_Weekend_13": 0.023580,
		"Feb_Weekend_14": 0.019667,
		"Feb_Weekend_15": 0.021230,
		"Feb_Weekend_16": 0.057872,
		"Feb_Weekend_17": 0.100100,
		"Feb_Weekend_18": 0.105638,
		"Feb_Weekend_19": 0.093402,
		"Feb_Weekend_2":  0.086448,
		"Feb_Weekend_20": 0.090060,
		"Feb_Weekend_21": 0.085313,
		"Feb_Weekend_22": 0.086765,
		"Feb_Weekend_23": 0.091190,
		"Feb_Weekend_3":  0.086280,
		"Feb_Weekend_4":  0.084991,
		"Feb_Weekend_5":  0.086238,
		"Feb_Weekend_6":  0.084268,
		"Feb_Weekend_7":  0.072650,
		"Feb_Weekend_8":  0.044985,
		"Feb_Weekend_9":  0.029052,
		"Jan_Weekday_0":  0.090860,
		"Jan_Weekday_1":  0.087496,
		"Jan_Weekday_10": 0.061488,
		"Jan_Weekday_11": 0.059848,
		"Jan_Weekday_12": 0.058431,
		"Jan_Weekday_13": 0.056537,
		"Jan_Weekday_14": 0.052843,
		"Jan_Weekday_15": 0.059205,
		"Jan_Weekday_16": 0.080443,
		"Jan_Weekday_17": 0.111501,
		"Jan_Weekday_18": 0.103883,
		"Jan_Weekday_19": 0.091598,
		"Jan_Weekday_2":  0.086219,
		"Jan_Weekday_20": 0.091150,
		"Jan_Weekday_21": 0.090949,
		"Jan_Weekday_22": 0.094930,
		"Jan_Weekday_23": 0.093007,
		"Jan_Weekday_3":  0.084807,
		"Jan_Weekday_4":  0.086897,
		"Jan_Weekday_5":  0.091765,
		"Jan_Weekday_6":  0.103974,
		"Jan_Weekday_7":  0.089217,
		"Jan_Weekday_8":  0.072630,
		"Jan_Weekday_9":  0.066337,
		"Jan_Weekend_0":  0.091341,
		"Jan_Weekend_1":  0.091661,
		"Jan_Weekend_10": 0.051001,
		"Jan_Weekend_11": 0.050683,
		"Jan_Weekend_12": 0.051148,
		"Jan_Weekend_13": 0.048233,
		"Jan_Weekend_14": 0.049743,
		"Jan_Weekend_15": 0.055345,
		"Jan_Weekend_16": 0.081593,
		"Jan_Weekend_17": 0.107017,
		"Jan_Weekend_18": 0.099423,
		"Jan_Weekend_19": 0.092253,
		"Jan_Weekend_2":  0.090548,
		"Jan_Weekend_20": 0.089519,
		"Jan_Weekend_21": 0.085986,
		"Jan_Weekend_22": 0.091648,
		"Jan_Weekend_23": 0.088033,
		"Jan_Weekend_3":  0.087895,
		"Jan_Weekend_4":  0.087078,
		"Jan_Weekend_5":  0.088566,
		"Jan_Weekend_6":  0.090989,
		"Jan_Weekend_7":  0.082643,
		"Jan_Weekend_8":  0.067661,
		"Jan_Weekend_9":  0.056176,
		"Jul_Weekday_0":  0.077135,
		"Jul_Weekday_1":  0.078689,
		"Jul_Weekday_10": 0.054487,
		"Jul_Weekday_11": 0.051968,
		"Jul_Weekday_12": 0.052473,
		"Jul_Weekday_13": 0.049589,
		"Jul_Weekday_14": 0.047911,
		"Jul_Weekday_15": 0.047054,
		"Jul_Weekday_16": 0.153534,
		"Jul_Weekday_17": 0.283348,
		"Jul_Weekday_18": 0.329480,
		"Jul_Weekday_19": 0.337384,
		"Jul_Weekday_2":  0.073178,
		"Jul_Weekday_20": 0.211887,
		"Jul_Weekday_21": 0.084427,
		"Jul_Weekday_22": 0.079866,
		"Jul_Weekday_23": 0.079257,
		"Jul_Weekday_3":  0.069173,
		"Jul_Weekday_4":  0.068854,
		"Jul_Weekday_5":  0.071495,
		"Jul_Weekday_6":  0.072767,
		"Jul_Weekday_7":  0.069536,
		"Jul_Weekday_8":  0.053684,
		"Jul_Weekday_9":  0.053241,
		"Jul_Weekend_0":  0.080483,
		"Jul_Weekend_1":  0.074510,
		"Jul_Weekend_10": 0.021775,
		"Jul_Weekend_11": 0.020726,
		"Jul_Weekend_12": 0.018435,
		"Jul_Weekend_13": 0.016830,
		"Jul_Weekend_14": 0.017190,
		"Jul_Weekend_15": 0.018484,
		"Jul_Weekend_16": 0.022857,
		"Jul_Weekend_17": 0.043278,
		"Jul_Weekend_18": 0.099714,
		"Jul_Weekend_19": 0.132700,
		"Jul_Weekend_2":  0.068784,
		"Jul_Weekend_20": 0.120154,
		"Jul_Weekend_21": 0.080413,
		"Jul_Weekend_22": 0.080238,
		"Jul_Weekend_23": 0.078425,
		"Jul_Weekend_3":  0.066396,
		"Jul_Weekend_4":  0.069235,
		"Jul_Weekend_5":  0.068046,
		"Jul_Weekend_6":  0.065883,
		"Jul_Weekend_7":  0.050023,
		"Jul_Weekend_8":  0.020292,
		"Jul_Weekend_9":  0.018778,
		"Jun_Weekday_0":  0.078983,
		"Jun_Weekday_1":  0.078576,
		"Jun_Weekday_10": 0.028895,
		"Jun_Weekday_11": 0.029717,
		"Jun_Weekday_12": 0.025307,
		"Jun_Weekday_13": 0.023066,
		"Jun_Weekday_14": 0.022505,
		"Jun_Weekday_15": 0.025216,
		"Jun_Weekday_16": 0.025429,
		"Jun_Weekday_17": 0.041357,
		"Jun_Weekday_18": 0.089748,
		"Jun_Weekday_19": 0.099167,
		"Jun_Weekday_2":  0.078383,
		"Jun_Weekday_20": 0.088224,
		"Jun_Weekday_21": 0.085812,
		"Jun_Weekday_22": 0.077531,
		"Jun_Weekday_23": 0.073513,
		"Jun_Weekday_3":  0.074198,
		"Jun_Weekday_4":  0.075727,
		"Jun_Weekday_5":  0.076613,
		"Jun_Weekday_6":  0.073365,
		"Jun_Weekday_7":  0.063311,
		"Jun_Weekday_8":  0.039593,
		"Jun_Weekday_9":  0.032138,
		"Jun_Weekend_0":  0.071171,
		"Jun_Weekend_1":  0.079215,
		"Jun_Weekend_10": 0.012991,
		"Jun_Weekend_11": 0.009546,
		"Jun_Weekend_12": 0.010395,
		"Jun_Weekend_13": 0.008646,
		"Jun_Weekend_14": 0.006408,
		"Jun_Weekend_15": 0.007201,
		"Jun_Weekend_16": 0.008532,
		"Jun_Weekend_17": 0.021594,
		"Jun_Weekend_18": 0.073314,
		"Jun_Weekend_19": 0.099890,
		"Jun_Weekend_2":  0.076676,
		"Jun_Weekend_20": 0.097097,
		"Jun_Weekend_21": 0.084338,
		"Jun_Weekend_22": 0.082019,
		"Jun_Weekend_23": 0.076756,
		"Jun_Weekend_3":  0.074893,
		"Jun_Weekend_4":  0.070256,
		"Jun_Weekend_5":  0.071617,
		"Jun_Weekend_6":  0.074675,
		"Jun_Weekend_7":  0.043598,
		"Jun_Weekend_8":  0.010008,
		"Jun_Weekend_9":  0.010261,
		"Mar_Weekday_0":  0.078858,
		"Mar_Weekday_1":  0.076105,
		"Mar_Weekday_10": 0.019250,
		"Mar_Weekday_11": 0.018106,
		"Mar_Weekday_12": 0.017981,
		"Mar_Weekday_13": 0.011862,
		"Mar_Weekday_14": 0.008004,
		"Mar_Weekday_15": 0.012817,
		"Mar_Weekday_16": 0.023909,
		"Mar_Weekday_17": 0.057780,
		"Mar_Weekday_18": 0.086626,
		"Mar_Weekday_19": 0.083255,
		"Mar_Weekday_2":  0.075356,
		"Mar_Weekday_20": 0.078399,
		"Mar_Weekday_21": 0.076547,
		"Mar_Weekday_22": 0.075259,
		"Mar_Weekday_23": 0.078580,
		"Mar_Weekday_3":  0.076048,
		"Mar_Weekday_4":  0.078034,
		"Mar_Weekday_5":  0.079613,
		"Mar_Weekday_6":  0.075042,
		"Mar_Weekday_7":  0.065949,
		"Mar_Weekday_8":  0.051202,
		"Mar_Weekday_9":  0.022170,
		"Mar_Weekend_0":  0.073500,
		"Mar_Weekend_1":  0.074453,
		"Mar_Weekend_10": 0.003013,
		"Mar_Weekend_11": 0.004142,
		"Mar_Weekend_12": 0.001968,
		"Mar_Weekend_13": 0.001982,
		"Mar_Weekend_14": 0.000000,
		"Mar_Weekend_15": 0.000000,
		"Mar_Weekend_16": 0.014691,
		"Mar_Weekend_17": 0.029991,
		"Mar_Weekend_18": 0.079424,
		"Mar_Weekend_19": 0.083944,
		"Mar_Weekend_2":  0.080435,
		"Mar_Weekend_20": 0.074379,
		"Mar_Weekend_21": 0.077167,
		"Mar_Weekend_22": 0.079179,
		"Mar_Weekend_23": 0.074569,
		"Mar_Weekend_3":  0.079237,
		"Mar_Weekend_4":  0.085324,
		"Mar_Weekend_5":  0.083995,
		"Mar_Weekend_6":  0.075574,
		"Mar_Weekend_7":  0.064555,
		"Mar_Weekend_8":  0.007303,
		"Mar_Weekend_9":  0.005490,
		"May_Weekday_0":  0.080002,
		"May_Weekday_1":  0.076327,
		"May_Weekday_10": 0.011113,
		"May_Weekday_11": 0.010204,
		"May_Weekday_12": 0.009652,
		"May_Weekday_13": 0.006612,
		"May_Weekday_14": 0.005108,
		"May_Weekday_15": 0.005366,
		"May_Weekday_16": 0.008971,
		"May_Weekday_17": 0.028665,
		"May_Weekday_18": 0.081682,
		"May_Weekday_19": 0.084745,
		"May_Weekday_2":  0.078920,
		"May_Weekday_20": 0.076853,
		"May_Weekday_21": 0.075588,
		"May_Weekday_22": 0.074489,
		"May_Weekday_23": 0.073521,
		"May_Weekday_3":  0.082373,
		"May_Weekday_4":  0.083141,
		"May_Weekday_5":  0.077452,
		"May_Weekday_6":  0.067519,
		"May_Weekday_7":  0.055576,
		"May_Weekday_8":  0.018516,
		"May_Weekday_9":  0.008385,
		"May_Weekend_0":  0.068072,
		"May_Weekend_1":  0.072837,
		"May_Weekend_10": 0.002130,
		"May_Weekend_11": 0.004404,
		"May_Weekend_12": 0.004323,
		"May_Weekend_13": 0.000000,
		"May_Weekend_14": 0.000000,
		"May_Weekend_15": 0.000000,
		"May_Weekend_16": 0.000000,
		"May_Weekend_17": 0.004684,
		"May_Weekend_18": 0.048006,
		"May_Weekend_19": 0.079516,
		"May_Weekend_2":  0.085522,
		"May_Weekend_20": 0.072391,
		"May_Weekend_21": 0.070975,
		"May_Weekend_22": 0.074593,
		"May_Weekend_23": 0.072127,
		"May_Weekend_3":  0.080831,
		"May_Weekend_4":  0.078338,
		"May_Weekend_5":  0.072442,
		"May_Weekend_6":  0.076309,
		"May_Weekend_7":  0.018967,
		"May_Weekend_8":  0.000774,
		"May_Weekend_9":  0.002141,
		"Nov_Weekday_0":  0.086774,
		"Nov_Weekday_1":  0.084461,
		"Nov_Weekday_10": 0.056142,
		"Nov_Weekday_11": 0.052810,
		"Nov_Weekday_12": 0.049765,
		"Nov_Weekday_13": 0.046108,
		"Nov_Weekday_14": 0.044399,
		"Nov_Weekday_15": 0.060404,
		"Nov_Weekday_16": 0.095497,
		"Nov_Weekday_17": 0.095279,
		"Nov_Weekday_18": 0.086694,
		"Nov_Weekday_19": 0.083083,
		"Nov_Weekday_2":  0.081954,
		"Nov_Weekday_20": 0.082265,
		"Nov_Weekday_21": 0.086921,
		"Nov_Weekday_22": 0.089904,
		"Nov_Weekday_23": 0.087720,
		"Nov_Weekday_3":  0.080843,
		"Nov_Weekday_4":  0.082454,
		"Nov_Weekday_5":  0.086139,
		"Nov_Weekday_6":  0.080109,
		"Nov_Weekday_7":  0.071710,
		"Nov_Weekday_8":  0.062092,
		"Nov_Weekday_9":  0.059831,
		"Nov_Weekend_0":  0.081461,
		"Nov_Weekend_1":  0.077590,
		"Nov_Weekend_10": 0.022442,
		"Nov_Weekend_11": 0.021269,
		"Nov_Weekend_12": 0.017075,
		"Nov_Weekend_13": 0.018323,
		"Nov_Weekend_14": 0.019430,
		"Nov_Weekend_15": 0.036147,
		"Nov_Weekend_16": 0.077826,
		"Nov_Weekend_17": 0.082273,
		"Nov_Weekend_18": 0.081250,
		"Nov_Weekend_19": 0.076330,
		"Nov_Weekend_2":  0.074947,
		"Nov_Weekend_20": 0.074833,
		"Nov_Weekend_21": 0.076885,
		"Nov_Weekend_22": 0.080967,
		"Nov_Weekend_23": 0.079054,
		"Nov_Weekend_3":  0.073996,
		"Nov_Weekend_4":  0.073898,
		"Nov_Weekend_5":  0.075209,
		"Nov_Weekend_6":  0.076285,
		"Nov_Weekend_7":  0.054631,
		"Nov_Weekend_8":  0.031379,
		"Nov_Weekend_9":  0.026576,
		"Oct_Weekday_0":  0.090020,
		"Oct_Weekday_1":  0.088370,
		"Oct_Weekday_10": 0.055152,
		"Oct_Weekday_11": 0.052607,
		"Oct_Weekday_12": 0.052319,
		"Oct_Weekday_13": 0.051989,
		"Oct_Weekday_14": 0.050634,
		"Oct_Weekday_15": 0.050941,
		"Oct_Weekday_16": 0.055201,
		"Oct_Weekday_17": 0.191084,
		"Oct_Weekday_18": 0.200706,
		"Oct_Weekday_19": 0.094043,
		"Oct_Weekday_2":  0.085824,
		"Oct_Weekday_20": 0.089743,
		"Oct_Weekday_21": 0.085168,
		"Oct_Weekday_22": 0.092400,
		"Oct_Weekday_23": 0.094045,
		"Oct_Weekday_3":  0.082670,
		"Oct_Weekday_4":  0.080972,
		"Oct_Weekday_5":  0.082700,
		"Oct_Weekday_6":  0.086926,
		"Oct_Weekday_7":  0.074720,
		"Oct_Weekday_8":  0.063577,
		"Oct_Weekday_9":  0.056589,
		"Oct_Weekend_0":  0.080574,
		"Oct_Weekend_1":  0.082821,
		"Oct_Weekend_10": 0.019300,
		"Oct_Weekend_11": 0.017690,
		"Oct_Weekend_12": 0.017659,
		"Oct_Weekend_13": 0.016134,
		"Oct_Weekend_14": 0.017219,
		"Oct_Weekend_15": 0.016619,
		"Oct_Weekend_16": 0.041033,
		"Oct_Weekend_17": 0.083263,
		"Oct_Weekend_18": 0.085048,
		"Oct_Weekend_19": 0.085254,
		"Oct_Weekend_2":  0.082032,
		"Oct_Weekend_20": 0.082051,
		"Oct_Weekend_21": 0.080439,
		"Oct_Weekend_22": 0.092783,
		"Oct_Weekend_23": 0.086877,
		"Oct_Weekend_3":  0.079366,
		"Oct_Weekend_4":  0.080672,
		"Oct_Weekend_5":  0.078596,
		"Oct_Weekend_6":  0.072882,
		"Oct_Weekend_7":  0.068405,
		"Oct_Weekend_8":  0.044382,
		"Oct_Weekend_9":  0.018008,
		"Sep_Weekday_0":  0.115484,
		"Sep_Weekday_1":  0.092921,
		"Sep_Weekday_10": 0.060932,
		"Sep_Weekday_11": 0.060303,
		"Sep_Weekday_12": 0.059002,
		"Sep_Weekday_13": 0.058515,
		"Sep_Weekday_14": 0.057523,
		"Sep_Weekday_15": 0.057565,
		"Sep_Weekday_16": 0.074864,
		"Sep_Weekday_17": 0.172104,
		"Sep_Weekday_18": 0.587471,
		"Sep_Weekday_19": 0.691370,
		"Sep_Weekday_2":  0.091330,
		"Sep_Weekday_20": 0.370999,
		"Sep_Weekday_21": 0.210960,
		"Sep_Weekday_22": 0.209659,
		"Sep_Weekday_23": 0.134746,
		"Sep_Weekday_3":  0.088592,
		"Sep_Weekday_4":  0.087194,
		"Sep_Weekday_5":  0.089046,
		"Sep_Weekday_6":  0.083780,
		"Sep_Weekday_7":  0.072818,
		"Sep_Weekday_8":  0.063836,
		"Sep_Weekday_9":  0.062135,
		"Sep_Weekend_0":  0.111627,
		"Sep_Weekend_1":  0.086336,
		"Sep_Weekend_10": 0.017552,
		"Sep_Weekend_11": 0.021351,
		"Sep_Weekend_12": 0.022251,
		"Sep_Weekend_13": 0.019712,
		"Sep_Weekend_14": 0.026347,
		"Sep_Weekend_15": 0.028044,
		"Sep_Weekend_16": 0.246806,
		"Sep_Weekend_17": 0.591685,
		"Sep_Weekend_18": 1.109707,
		"Sep_Weekend_19": 1.031940,
		"Sep_Weekend_2":  0.081526,
		"Sep_Weekend_20": 0.619491,
		"Sep_Weekend_21": 0.236525,
		"Sep_Weekend_22": 0.235798,
		"Sep_Weekend_23": 0.143140,
		"Sep_Weekend_3":  0.079488,
		"Sep_Weekend_4":  0.077737,
		"Sep_Weekend_5":  0.078019,
		"Sep_Weekend_6":  0.076088,
		"Sep_Weekend_7":  0.062658,
		"Sep_Weekend_8":  0.031371,
		"Sep_Weekend_9":  0.019684,
	},
}

// sdgeNBT27Data contains Avoided Cost Calculator (ACC) NBT27 and 2027 NBT00 export rates ($/kWh)
// for 2027, representing the sum of Generation (USCA-XXSD) and Delivery (USCA-SDXX) credit rates,
// grouped by Month_DayType_Hour.
var sdgeNBT27Data = map[string]float64{
	"Apr_Weekday_0":  0.181317,
	"Apr_Weekday_1":  0.173665,
	"Apr_Weekday_10": 0.011383,
	"Apr_Weekday_11": 0.005415,
	"Apr_Weekday_12": 0.003316,
	"Apr_Weekday_13": 0.002064,
	"Apr_Weekday_14": 0.002064,
	"Apr_Weekday_15": 0.002342,
	"Apr_Weekday_16": 0.000596,
	"Apr_Weekday_17": 0.006060,
	"Apr_Weekday_18": 0.134759,
	"Apr_Weekday_19": 0.174677,
	"Apr_Weekday_2":  0.170850,
	"Apr_Weekday_20": 0.181035,
	"Apr_Weekday_21": 0.186513,
	"Apr_Weekday_22": 0.189702,
	"Apr_Weekday_23": 0.181375,
	"Apr_Weekday_3":  0.175340,
	"Apr_Weekday_4":  0.174035,
	"Apr_Weekday_5":  0.176841,
	"Apr_Weekday_6":  0.168186,
	"Apr_Weekday_7":  0.084584,
	"Apr_Weekday_8":  0.019093,
	"Apr_Weekday_9":  0.006105,
	"Apr_Weekend_0":  0.172600,
	"Apr_Weekend_1":  0.172196,
	"Apr_Weekend_10": 0.002472,
	"Apr_Weekend_11": 0.001415,
	"Apr_Weekend_12": 0.001415,
	"Apr_Weekend_13": 0.000000,
	"Apr_Weekend_14": 0.000000,
	"Apr_Weekend_15": 0.000000,
	"Apr_Weekend_16": 0.000000,
	"Apr_Weekend_17": 0.000000,
	"Apr_Weekend_18": 0.112154,
	"Apr_Weekend_19": 0.150979,
	"Apr_Weekend_2":  0.173262,
	"Apr_Weekend_20": 0.164540,
	"Apr_Weekend_21": 0.185855,
	"Apr_Weekend_22": 0.187717,
	"Apr_Weekend_23": 0.183950,
	"Apr_Weekend_3":  0.164411,
	"Apr_Weekend_4":  0.159351,
	"Apr_Weekend_5":  0.160873,
	"Apr_Weekend_6":  0.161912,
	"Apr_Weekend_7":  0.047431,
	"Apr_Weekend_8":  0.009171,
	"Apr_Weekend_9":  0.003437,
	"Aug_Weekday_0":  0.227664,
	"Aug_Weekday_1":  0.218378,
	"Aug_Weekday_10": 0.141937,
	"Aug_Weekday_11": 0.139678,
	"Aug_Weekday_12": 0.140838,
	"Aug_Weekday_13": 0.147669,
	"Aug_Weekday_14": 0.181391,
	"Aug_Weekday_15": 0.197787,
	"Aug_Weekday_16": 0.291655,
	"Aug_Weekday_17": 0.380858,
	"Aug_Weekday_18": 0.446042,
	"Aug_Weekday_19": 0.406681,
	"Aug_Weekday_2":  0.211030,
	"Aug_Weekday_20": 0.360711,
	"Aug_Weekday_21": 0.344995,
	"Aug_Weekday_22": 0.334518,
	"Aug_Weekday_23": 0.302720,
	"Aug_Weekday_3":  0.207350,
	"Aug_Weekday_4":  0.206432,
	"Aug_Weekday_5":  0.218736,
	"Aug_Weekday_6":  0.218230,
	"Aug_Weekday_7":  0.187539,
	"Aug_Weekday_8":  0.154171,
	"Aug_Weekday_9":  0.147729,
	"Aug_Weekend_0":  0.233485,
	"Aug_Weekend_1":  0.223497,
	"Aug_Weekend_10": 0.072205,
	"Aug_Weekend_11": 0.066922,
	"Aug_Weekend_12": 0.069263,
	"Aug_Weekend_13": 0.080660,
	"Aug_Weekend_14": 0.086593,
	"Aug_Weekend_15": 0.102341,
	"Aug_Weekend_16": 0.129062,
	"Aug_Weekend_17": 0.241868,
	"Aug_Weekend_18": 0.304880,
	"Aug_Weekend_19": 0.269184,
	"Aug_Weekend_2":  0.214469,
	"Aug_Weekend_20": 0.252808,
	"Aug_Weekend_21": 0.248212,
	"Aug_Weekend_22": 0.264294,
	"Aug_Weekend_23": 0.247513,
	"Aug_Weekend_3":  0.220319,
	"Aug_Weekend_4":  0.209239,
	"Aug_Weekend_5":  0.216268,
	"Aug_Weekend_6":  0.209372,
	"Aug_Weekend_7":  0.147954,
	"Aug_Weekend_8":  0.104603,
	"Aug_Weekend_9":  0.081765,
	"Dec_Weekday_0":  0.200642,
	"Dec_Weekday_1":  0.196624,
	"Dec_Weekday_10": 0.156698,
	"Dec_Weekday_11": 0.150768,
	"Dec_Weekday_12": 0.145230,
	"Dec_Weekday_13": 0.136496,
	"Dec_Weekday_14": 0.135908,
	"Dec_Weekday_15": 0.163914,
	"Dec_Weekday_16": 0.243468,
	"Dec_Weekday_17": 0.241389,
	"Dec_Weekday_18": 0.238025,
	"Dec_Weekday_19": 0.242272,
	"Dec_Weekday_2":  0.193026,
	"Dec_Weekday_20": 0.234196,
	"Dec_Weekday_21": 0.228302,
	"Dec_Weekday_22": 0.222841,
	"Dec_Weekday_23": 0.207119,
	"Dec_Weekday_3":  0.194799,
	"Dec_Weekday_4":  0.205439,
	"Dec_Weekday_5":  0.218386,
	"Dec_Weekday_6":  0.234745,
	"Dec_Weekday_7":  0.198856,
	"Dec_Weekday_8":  0.179155,
	"Dec_Weekday_9":  0.171286,
	"Dec_Weekend_0":  0.194106,
	"Dec_Weekend_1":  0.191227,
	"Dec_Weekend_10": 0.063736,
	"Dec_Weekend_11": 0.062770,
	"Dec_Weekend_12": 0.058988,
	"Dec_Weekend_13": 0.055306,
	"Dec_Weekend_14": 0.049511,
	"Dec_Weekend_15": 0.068547,
	"Dec_Weekend_16": 0.267522,
	"Dec_Weekend_17": 0.240240,
	"Dec_Weekend_18": 0.231589,
	"Dec_Weekend_19": 0.229456,
	"Dec_Weekend_2":  0.189497,
	"Dec_Weekend_20": 0.225340,
	"Dec_Weekend_21": 0.214653,
	"Dec_Weekend_22": 0.207195,
	"Dec_Weekend_23": 0.198254,
	"Dec_Weekend_3":  0.187681,
	"Dec_Weekend_4":  0.186734,
	"Dec_Weekend_5":  0.189915,
	"Dec_Weekend_6":  0.199084,
	"Dec_Weekend_7":  0.149346,
	"Dec_Weekend_8":  0.087225,
	"Dec_Weekend_9":  0.087508,
	"Feb_Weekday_0":  0.208980,
	"Feb_Weekday_1":  0.205462,
	"Feb_Weekday_10": 0.058759,
	"Feb_Weekday_11": 0.048968,
	"Feb_Weekday_12": 0.043798,
	"Feb_Weekday_13": 0.044537,
	"Feb_Weekday_14": 0.039797,
	"Feb_Weekday_15": 0.037707,
	"Feb_Weekday_16": 0.101013,
	"Feb_Weekday_17": 0.257951,
	"Feb_Weekday_18": 0.243801,
	"Feb_Weekday_19": 0.245383,
	"Feb_Weekday_2":  0.204512,
	"Feb_Weekday_20": 0.238364,
	"Feb_Weekday_21": 0.230968,
	"Feb_Weekday_22": 0.222099,
	"Feb_Weekday_23": 0.214337,
	"Feb_Weekday_3":  0.203029,
	"Feb_Weekday_4":  0.215283,
	"Feb_Weekday_5":  0.234176,
	"Feb_Weekday_6":  0.245691,
	"Feb_Weekday_7":  0.219155,
	"Feb_Weekday_8":  0.088154,
	"Feb_Weekday_9":  0.061233,
	"Feb_Weekend_0":  0.212719,
	"Feb_Weekend_1":  0.211859,
	"Feb_Weekend_10": 0.052807,
	"Feb_Weekend_11": 0.050593,
	"Feb_Weekend_12": 0.039480,
	"Feb_Weekend_13": 0.043630,
	"Feb_Weekend_14": 0.034005,
	"Feb_Weekend_15": 0.035051,
	"Feb_Weekend_16": 0.110885,
	"Feb_Weekend_17": 0.267839,
	"Feb_Weekend_18": 0.238872,
	"Feb_Weekend_19": 0.240483,
	"Feb_Weekend_2":  0.202808,
	"Feb_Weekend_20": 0.231846,
	"Feb_Weekend_21": 0.233574,
	"Feb_Weekend_22": 0.226712,
	"Feb_Weekend_23": 0.207760,
	"Feb_Weekend_3":  0.201304,
	"Feb_Weekend_4":  0.205074,
	"Feb_Weekend_5":  0.225293,
	"Feb_Weekend_6":  0.221511,
	"Feb_Weekend_7":  0.187155,
	"Feb_Weekend_8":  0.088684,
	"Feb_Weekend_9":  0.059799,
	"Jan_Weekday_0":  0.195223,
	"Jan_Weekday_1":  0.191520,
	"Jan_Weekday_10": 0.068792,
	"Jan_Weekday_11": 0.060484,
	"Jan_Weekday_12": 0.056127,
	"Jan_Weekday_13": 0.044459,
	"Jan_Weekday_14": 0.041644,
	"Jan_Weekday_15": 0.056936,
	"Jan_Weekday_16": 0.192292,
	"Jan_Weekday_17": 0.238280,
	"Jan_Weekday_18": 0.231693,
	"Jan_Weekday_19": 0.230219,
	"Jan_Weekday_2":  0.189176,
	"Jan_Weekday_20": 0.227931,
	"Jan_Weekday_21": 0.222480,
	"Jan_Weekday_22": 0.211576,
	"Jan_Weekday_23": 0.202460,
	"Jan_Weekday_3":  0.188629,
	"Jan_Weekday_4":  0.195638,
	"Jan_Weekday_5":  0.209556,
	"Jan_Weekday_6":  0.234781,
	"Jan_Weekday_7":  0.205728,
	"Jan_Weekday_8":  0.131445,
	"Jan_Weekday_9":  0.104540,
	"Jan_Weekend_0":  0.186337,
	"Jan_Weekend_1":  0.181554,
	"Jan_Weekend_10": 0.035868,
	"Jan_Weekend_11": 0.031638,
	"Jan_Weekend_12": 0.020350,
	"Jan_Weekend_13": 0.004668,
	"Jan_Weekend_14": 0.014326,
	"Jan_Weekend_15": 0.039964,
	"Jan_Weekend_16": 0.147734,
	"Jan_Weekend_17": 0.216387,
	"Jan_Weekend_18": 0.217077,
	"Jan_Weekend_19": 0.214488,
	"Jan_Weekend_2":  0.179641,
	"Jan_Weekend_20": 0.212574,
	"Jan_Weekend_21": 0.202603,
	"Jan_Weekend_22": 0.193818,
	"Jan_Weekend_23": 0.183485,
	"Jan_Weekend_3":  0.176266,
	"Jan_Weekend_4":  0.178098,
	"Jan_Weekend_5":  0.182424,
	"Jan_Weekend_6":  0.191941,
	"Jan_Weekend_7":  0.150524,
	"Jan_Weekend_8":  0.070675,
	"Jan_Weekend_9":  0.058308,
	"Jul_Weekday_0":  0.216226,
	"Jul_Weekday_1":  0.205901,
	"Jul_Weekday_10": 0.103544,
	"Jul_Weekday_11": 0.103203,
	"Jul_Weekday_12": 0.101256,
	"Jul_Weekday_13": 0.107792,
	"Jul_Weekday_14": 0.115796,
	"Jul_Weekday_15": 0.123369,
	"Jul_Weekday_16": 0.144098,
	"Jul_Weekday_17": 0.219132,
	"Jul_Weekday_18": 0.297572,
	"Jul_Weekday_19": 0.277459,
	"Jul_Weekday_2":  0.197361,
	"Jul_Weekday_20": 0.259450,
	"Jul_Weekday_21": 0.257443,
	"Jul_Weekday_22": 0.252736,
	"Jul_Weekday_23": 0.236043,
	"Jul_Weekday_3":  0.192699,
	"Jul_Weekday_4":  0.192174,
	"Jul_Weekday_5":  0.199361,
	"Jul_Weekday_6":  0.193153,
	"Jul_Weekday_7":  0.166199,
	"Jul_Weekday_8":  0.129817,
	"Jul_Weekday_9":  0.121210,
	"Jul_Weekend_0":  0.218194,
	"Jul_Weekend_1":  0.208776,
	"Jul_Weekend_10": 0.051166,
	"Jul_Weekend_11": 0.031297,
	"Jul_Weekend_12": 0.036926,
	"Jul_Weekend_13": 0.035361,
	"Jul_Weekend_14": 0.043298,
	"Jul_Weekend_15": 0.043500,
	"Jul_Weekend_16": 0.063338,
	"Jul_Weekend_17": 0.138195,
	"Jul_Weekend_18": 0.286046,
	"Jul_Weekend_19": 0.274097,
	"Jul_Weekend_2":  0.197908,
	"Jul_Weekend_20": 0.238047,
	"Jul_Weekend_21": 0.236540,
	"Jul_Weekend_22": 0.249760,
	"Jul_Weekend_23": 0.236660,
	"Jul_Weekend_3":  0.192419,
	"Jul_Weekend_4":  0.191983,
	"Jul_Weekend_5":  0.193591,
	"Jul_Weekend_6":  0.174467,
	"Jul_Weekend_7":  0.116104,
	"Jul_Weekend_8":  0.060789,
	"Jul_Weekend_9":  0.050459,
	"Jun_Weekday_0":  0.194984,
	"Jun_Weekday_1":  0.187654,
	"Jun_Weekday_10": 0.041272,
	"Jun_Weekday_11": 0.040005,
	"Jun_Weekday_12": 0.040381,
	"Jun_Weekday_13": 0.077826,
	"Jun_Weekday_14": 0.044665,
	"Jun_Weekday_15": 0.045099,
	"Jun_Weekday_16": 0.054874,
	"Jun_Weekday_17": 0.118313,
	"Jun_Weekday_18": 0.248355,
	"Jun_Weekday_19": 0.237819,
	"Jun_Weekday_2":  0.183550,
	"Jun_Weekday_20": 0.234632,
	"Jun_Weekday_21": 0.236390,
	"Jun_Weekday_22": 0.225074,
	"Jun_Weekday_23": 0.214873,
	"Jun_Weekday_3":  0.180035,
	"Jun_Weekday_4":  0.179471,
	"Jun_Weekday_5":  0.184973,
	"Jun_Weekday_6":  0.180003,
	"Jun_Weekday_7":  0.098321,
	"Jun_Weekday_8":  0.066986,
	"Jun_Weekday_9":  0.054629,
	"Jun_Weekend_0":  0.192503,
	"Jun_Weekend_1":  0.184329,
	"Jun_Weekend_10": 0.018178,
	"Jun_Weekend_11": 0.016499,
	"Jun_Weekend_12": 0.012030,
	"Jun_Weekend_13": 0.015998,
	"Jun_Weekend_14": 0.016235,
	"Jun_Weekend_15": 0.017646,
	"Jun_Weekend_16": 0.018344,
	"Jun_Weekend_17": 0.033373,
	"Jun_Weekend_18": 0.202982,
	"Jun_Weekend_19": 0.187499,
	"Jun_Weekend_2":  0.167444,
	"Jun_Weekend_20": 0.210119,
	"Jun_Weekend_21": 0.211147,
	"Jun_Weekend_22": 0.207013,
	"Jun_Weekend_23": 0.192304,
	"Jun_Weekend_3":  0.167025,
	"Jun_Weekend_4":  0.161413,
	"Jun_Weekend_5":  0.158925,
	"Jun_Weekend_6":  0.149306,
	"Jun_Weekend_7":  0.022270,
	"Jun_Weekend_8":  0.021240,
	"Jun_Weekend_9":  0.018162,
	"Mar_Weekday_0":  0.177701,
	"Mar_Weekday_1":  0.173076,
	"Mar_Weekday_10": 0.024913,
	"Mar_Weekday_11": 0.019073,
	"Mar_Weekday_12": 0.016279,
	"Mar_Weekday_13": 0.014591,
	"Mar_Weekday_14": 0.019736,
	"Mar_Weekday_15": 0.025743,
	"Mar_Weekday_16": 0.023916,
	"Mar_Weekday_17": 0.069770,
	"Mar_Weekday_18": 0.181281,
	"Mar_Weekday_19": 0.196915,
	"Mar_Weekday_2":  0.171308,
	"Mar_Weekday_20": 0.201475,
	"Mar_Weekday_21": 0.199172,
	"Mar_Weekday_22": 0.190302,
	"Mar_Weekday_23": 0.180855,
	"Mar_Weekday_3":  0.172211,
	"Mar_Weekday_4":  0.174547,
	"Mar_Weekday_5":  0.180673,
	"Mar_Weekday_6":  0.183521,
	"Mar_Weekday_7":  0.164376,
	"Mar_Weekday_8":  0.038908,
	"Mar_Weekday_9":  0.028211,
	"Mar_Weekend_0":  0.171588,
	"Mar_Weekend_1":  0.170714,
	"Mar_Weekend_10": 0.007101,
	"Mar_Weekend_11": 0.001441,
	"Mar_Weekend_12": 0.000481,
	"Mar_Weekend_13": 0.000000,
	"Mar_Weekend_14": 0.000000,
	"Mar_Weekend_15": 0.000000,
	"Mar_Weekend_16": 0.032201,
	"Mar_Weekend_17": 0.061414,
	"Mar_Weekend_18": 0.164906,
	"Mar_Weekend_19": 0.184669,
	"Mar_Weekend_2":  0.164417,
	"Mar_Weekend_20": 0.186965,
	"Mar_Weekend_21": 0.184288,
	"Mar_Weekend_22": 0.182223,
	"Mar_Weekend_23": 0.175992,
	"Mar_Weekend_3":  0.169576,
	"Mar_Weekend_4":  0.169947,
	"Mar_Weekend_5":  0.173740,
	"Mar_Weekend_6":  0.171932,
	"Mar_Weekend_7":  0.123377,
	"Mar_Weekend_8":  0.014370,
	"Mar_Weekend_9":  0.015360,
	"May_Weekday_0":  0.176239,
	"May_Weekday_1":  0.169717,
	"May_Weekday_10": 0.008269,
	"May_Weekday_11": 0.004094,
	"May_Weekday_12": 0.001604,
	"May_Weekday_13": 0.000797,
	"May_Weekday_14": 0.000784,
	"May_Weekday_15": 0.001258,
	"May_Weekday_16": 0.002954,
	"May_Weekday_17": 0.003841,
	"May_Weekday_18": 0.130113,
	"May_Weekday_19": 0.159678,
	"May_Weekday_2":  0.167124,
	"May_Weekday_20": 0.188981,
	"May_Weekday_21": 0.197130,
	"May_Weekday_22": 0.197681,
	"May_Weekday_23": 0.192096,
	"May_Weekday_3":  0.165123,
	"May_Weekday_4":  0.162374,
	"May_Weekday_5":  0.165053,
	"May_Weekday_6":  0.161035,
	"May_Weekday_7":  0.020092,
	"May_Weekday_8":  0.009884,
	"May_Weekday_9":  0.007855,
	"May_Weekend_0":  0.155406,
	"May_Weekend_1":  0.154594,
	"May_Weekend_10": 0.002005,
	"May_Weekend_11": 0.000479,
	"May_Weekend_12": 0.000000,
	"May_Weekend_13": 0.000000,
	"May_Weekend_14": 0.000000,
	"May_Weekend_15": 0.000000,
	"May_Weekend_16": 0.000000,
	"May_Weekend_17": 0.000000,
	"May_Weekend_18": 0.066753,
	"May_Weekend_19": 0.105886,
	"May_Weekend_2":  0.154837,
	"May_Weekend_20": 0.149172,
	"May_Weekend_21": 0.170287,
	"May_Weekend_22": 0.172341,
	"May_Weekend_23": 0.150158,
	"May_Weekend_3":  0.147557,
	"May_Weekend_4":  0.148630,
	"May_Weekend_5":  0.152636,
	"May_Weekend_6":  0.137015,
	"May_Weekend_7":  0.004565,
	"May_Weekend_8":  0.003918,
	"May_Weekend_9":  0.003440,
	"Nov_Weekday_0":  0.200057,
	"Nov_Weekday_1":  0.195443,
	"Nov_Weekday_10": 0.099603,
	"Nov_Weekday_11": 0.083676,
	"Nov_Weekday_12": 0.078585,
	"Nov_Weekday_13": 0.076547,
	"Nov_Weekday_14": 0.084335,
	"Nov_Weekday_15": 0.119758,
	"Nov_Weekday_16": 0.243462,
	"Nov_Weekday_17": 0.240312,
	"Nov_Weekday_18": 0.240817,
	"Nov_Weekday_19": 0.236808,
	"Nov_Weekday_2":  0.195560,
	"Nov_Weekday_20": 0.237756,
	"Nov_Weekday_21": 0.230106,
	"Nov_Weekday_22": 0.221359,
	"Nov_Weekday_23": 0.213845,
	"Nov_Weekday_3":  0.199004,
	"Nov_Weekday_4":  0.206404,
	"Nov_Weekday_5":  0.218953,
	"Nov_Weekday_6":  0.241636,
	"Nov_Weekday_7":  0.173085,
	"Nov_Weekday_8":  0.127769,
	"Nov_Weekday_9":  0.106233,
	"Nov_Weekend_0":  0.205827,
	"Nov_Weekend_1":  0.195046,
	"Nov_Weekend_10": 0.066076,
	"Nov_Weekend_11": 0.053108,
	"Nov_Weekend_12": 0.051129,
	"Nov_Weekend_13": 0.038388,
	"Nov_Weekend_14": 0.046059,
	"Nov_Weekend_15": 0.071693,
	"Nov_Weekend_16": 0.238958,
	"Nov_Weekend_17": 0.236637,
	"Nov_Weekend_18": 0.230713,
	"Nov_Weekend_19": 0.228752,
	"Nov_Weekend_2":  0.196142,
	"Nov_Weekend_20": 0.235399,
	"Nov_Weekend_21": 0.220767,
	"Nov_Weekend_22": 0.208971,
	"Nov_Weekend_23": 0.200965,
	"Nov_Weekend_3":  0.194808,
	"Nov_Weekend_4":  0.205719,
	"Nov_Weekend_5":  0.210469,
	"Nov_Weekend_6":  0.214054,
	"Nov_Weekend_7":  0.113615,
	"Nov_Weekend_8":  0.079129,
	"Nov_Weekend_9":  0.066301,
	"Oct_Weekday_0":  0.215235,
	"Oct_Weekday_1":  0.207812,
	"Oct_Weekday_10": 0.125042,
	"Oct_Weekday_11": 0.119241,
	"Oct_Weekday_12": 0.142525,
	"Oct_Weekday_13": 0.143543,
	"Oct_Weekday_14": 0.171684,
	"Oct_Weekday_15": 0.166664,
	"Oct_Weekday_16": 0.196238,
	"Oct_Weekday_17": 0.304907,
	"Oct_Weekday_18": 0.321641,
	"Oct_Weekday_19": 0.246797,
	"Oct_Weekday_2":  0.203329,
	"Oct_Weekday_20": 0.247166,
	"Oct_Weekday_21": 0.242938,
	"Oct_Weekday_22": 0.236516,
	"Oct_Weekday_23": 0.225671,
	"Oct_Weekday_3":  0.200048,
	"Oct_Weekday_4":  0.199439,
	"Oct_Weekday_5":  0.213588,
	"Oct_Weekday_6":  0.236649,
	"Oct_Weekday_7":  0.216974,
	"Oct_Weekday_8":  0.152609,
	"Oct_Weekday_9":  0.141249,
	"Oct_Weekend_0":  0.213796,
	"Oct_Weekend_1":  0.203025,
	"Oct_Weekend_10": 0.033930,
	"Oct_Weekend_11": 0.032501,
	"Oct_Weekend_12": 0.018212,
	"Oct_Weekend_13": 0.028636,
	"Oct_Weekend_14": 0.023749,
	"Oct_Weekend_15": 0.017025,
	"Oct_Weekend_16": 0.047772,
	"Oct_Weekend_17": 0.183584,
	"Oct_Weekend_18": 0.216150,
	"Oct_Weekend_19": 0.214660,
	"Oct_Weekend_2":  0.210967,
	"Oct_Weekend_20": 0.227577,
	"Oct_Weekend_21": 0.227416,
	"Oct_Weekend_22": 0.228790,
	"Oct_Weekend_23": 0.219430,
	"Oct_Weekend_3":  0.202186,
	"Oct_Weekend_4":  0.217262,
	"Oct_Weekend_5":  0.231785,
	"Oct_Weekend_6":  0.229403,
	"Oct_Weekend_7":  0.194116,
	"Oct_Weekend_8":  0.058238,
	"Oct_Weekend_9":  0.041254,
	"Sep_Weekday_0":  0.260477,
	"Sep_Weekday_1":  0.225123,
	"Sep_Weekday_10": 0.170471,
	"Sep_Weekday_11": 0.232540,
	"Sep_Weekday_12": 0.273112,
	"Sep_Weekday_13": 0.330989,
	"Sep_Weekday_14": 0.446597,
	"Sep_Weekday_15": 0.724944,
	"Sep_Weekday_16": 0.759330,
	"Sep_Weekday_17": 0.780437,
	"Sep_Weekday_18": 0.827056,
	"Sep_Weekday_19": 0.730269,
	"Sep_Weekday_2":  0.219092,
	"Sep_Weekday_20": 0.581148,
	"Sep_Weekday_21": 0.584366,
	"Sep_Weekday_22": 0.445924,
	"Sep_Weekday_23": 0.398540,
	"Sep_Weekday_3":  0.214334,
	"Sep_Weekday_4":  0.212913,
	"Sep_Weekday_5":  0.232826,
	"Sep_Weekday_6":  0.244190,
	"Sep_Weekday_7":  0.207696,
	"Sep_Weekday_8":  0.154705,
	"Sep_Weekday_9":  0.146402,
	"Sep_Weekend_0":  0.245854,
	"Sep_Weekend_1":  0.229394,
	"Sep_Weekend_10": 0.071268,
	"Sep_Weekend_11": 0.059772,
	"Sep_Weekend_12": 0.056690,
	"Sep_Weekend_13": 0.066518,
	"Sep_Weekend_14": 0.070359,
	"Sep_Weekend_15": 0.085681,
	"Sep_Weekend_16": 0.170935,
	"Sep_Weekend_17": 0.305277,
	"Sep_Weekend_18": 0.423807,
	"Sep_Weekend_19": 0.384424,
	"Sep_Weekend_2":  0.220695,
	"Sep_Weekend_20": 0.360202,
	"Sep_Weekend_21": 0.332323,
	"Sep_Weekend_22": 0.262889,
	"Sep_Weekend_23": 0.251636,
	"Sep_Weekend_3":  0.216183,
	"Sep_Weekend_4":  0.214649,
	"Sep_Weekend_5":  0.220438,
	"Sep_Weekend_6":  0.235322,
	"Sep_Weekend_7":  0.168939,
	"Sep_Weekend_8":  0.090761,
	"Sep_Weekend_9":  0.081959,
}

// getSDGEHolidays calculates designated holidays for SDG&E.
// According to San Diego Gas & Electric's official rate schedules and Solar Billing Plan export pricing guide:
// 1. New Year's Day (January 1)
// 2. Presidents' Day (Third Monday in February)
// 3. Memorial Day (Last Monday in May)
// 4. Independence Day (July 4)
// 5. Labor Day (First Monday in September)
// 6. Veterans Day (November 11)
// 7. Thanksgiving Day (Fourth Thursday in November)
// 8. Christmas Day (December 25)
//
// Shift rules:
// - If a holiday falls on a Sunday, the following Monday is recognized as the holiday.
// - If a holiday falls on a Saturday, it is not shifted (Saturday is already treated as weekend).
func getSDGEHolidays(year int) []string {
	shiftSDGEHoliday := func(t time.Time) time.Time {
		if t.Weekday() == time.Sunday {
			return t.AddDate(0, 0, 1)
		}
		return t
	}

	holidays := []time.Time{
		shiftSDGEHoliday(newYearsDay(year)),
		shiftSDGEHoliday(presidentsDay(year)),
		memorialDay(year),
		shiftSDGEHoliday(independenceDay(year)),
		laborDay(year),
		shiftSDGEHoliday(veteransDay(year)),
		thanksgivingDay(year),
		shiftSDGEHoliday(christmasDay(year)),
	}

	return formatHolidays(holidays, year)
}

// sdgeACCPlusAdderDollarsPerKWH returns the Solar Billing Plan ACC Plus export adder ($/kWh).
// Per CPUC Decision D.22-12-056 (Table 5 / p. 241), SDG&E ACC Plus adders are $0.00/kWh across all cohorts.
func sdgeACCPlusAdderDollarsPerKWH(_ string) float64 {
	return 0.0
}

// sdgeNBTBaseDollarsPerKWH returns the base EEC export credit ($/kWh, before ACC Plus adder)
// for a given calendar year, applicationYear cohort, and Month_DayType_Hour key.
func sdgeNBTBaseDollarsPerKWH(year int, applicationYear, key string) float64 {
	if year < 2026 {
		year = 2026
	} else if year > 2027 {
		year = 2027
	}

	switch strings.ToLower(strings.TrimSpace(applicationYear)) {
	case "2023", "2024":
		if yearMap, ok := sdgeNBT23Data[year]; ok {
			if val, ok := yearMap[key]; ok {
				return val
			}
		}
		if val, ok := sdgeNBT23Data[2026][key]; ok {
			return val
		}
	case "2027", "non_locked":
		if year >= 2027 {
			if val, ok := sdgeNBT27Data[key]; ok {
				return val
			}
		}
		if val, ok := sdgeNBTData[2026][key]; ok {
			return val
		}
	default:
		if yearMap, ok := sdgeNBTData[year]; ok {
			if val, ok := yearMap[key]; ok {
				return val
			}
		}
		if val, ok := sdgeNBTData[2026][key]; ok {
			return val
		}
	}

	return 0.05 // safe fallback
}

// getSDGENBTExportRate returns the export rate ($/kWh) for SDG&E Net Billing Tariff (Solar Billing Plan / NEM 3.0)
// at a given time and optional applicationYear cohort (defaulting to "2026").
func getSDGENBTExportRate(t time.Time, applicationYear ...string) float64 {
	if ptLocation != nil {
		t = t.In(ptLocation)
	}
	year := t.Year()
	if year < 2026 {
		year = 2026
	} else if year > 2027 {
		year = 2027
	}

	monthStr := t.Format("Jan")

	// Determine if weekend or holiday
	isWeekend := t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
	if !isWeekend {
		holidays := getSDGEHolidays(year)
		dateStr := t.Format("2006-01-02")
		for _, h := range holidays {
			if h == dateStr {
				isWeekend = true
				break
			}
		}
	}

	dayType := "Weekday"
	if isWeekend {
		dayType = "Weekend"
	}

	cohort := "2026"
	if len(applicationYear) > 0 && applicationYear[0] != "" {
		cohort = applicationYear[0]
	}

	key := fmt.Sprintf("%s_%s_%d", monthStr, dayType, t.Hour())
	return sdgeNBTBaseDollarsPerKWH(year, cohort, key) + sdgeACCPlusAdderDollarsPerKWH(cohort)
}

// Time-of-Use time period definitions and schedules sourced from https://www.sdge.com/residential/pricing-plans
// sdgePeriods generates the pricing periods for SDG&E / SDCP.
func sdgePeriods(plan string, options types.UtilityRateOptions, years []int) []types.UtilityFeesPeriod {
	var periods []types.UtilityFeesPeriod

	// Default options if empty
	genRate := options.GenerationRate
	if genRate == "" {
		genRate = "sdcp_power_on"
	}
	location := options.Location
	if location == "" {
		location = "san_diego"
	}

	udcEmbeddedNBCDollarsPerKWH := sdgeUDCEmbeddedStandardNBCDollarsPerKWH
	nemSTNBCDollarsPerKWH := sdgeNEMSTStandardNBCDollarsPerKWH
	if plan == "sdge_ev_tou" {
		udcEmbeddedNBCDollarsPerKWH = sdgeUDCEmbeddedEVTOUNBCDollarsPerKWH
		nemSTNBCDollarsPerKWH = sdgeNEMSTEVTOUNBCDollarsPerKWH
	}

	for _, year := range years {
		holidays := getSDGEHolidays(year)

		type sdgeSubSeason struct {
			season        string
			monthStart    time.Month
			monthEnd      time.Month
			effectiveDate time.Time
		}

		var subSeasons []sdgeSubSeason
		if year < 2026 {
			subSeasons = []sdgeSubSeason{
				{season: "summer", monthStart: time.June, monthEnd: time.October, effectiveDate: time.Date(year, time.June, 1, 0, 0, 0, 0, ptLocation)},
				{season: "winter", monthStart: time.November, monthEnd: time.May, effectiveDate: time.Date(year, time.November, 1, 0, 0, 0, 0, ptLocation)},
			}
		} else if year == 2026 {
			subSeasons = []sdgeSubSeason{
				{season: "winter", monthStart: time.January, monthEnd: time.March, effectiveDate: time.Date(2026, time.January, 1, 0, 0, 0, 0, ptLocation)},
				{season: "winter", monthStart: time.April, monthEnd: time.May, effectiveDate: time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation)},
				{season: "summer", monthStart: time.June, monthEnd: time.July, effectiveDate: time.Date(2026, time.June, 1, 0, 0, 0, 0, ptLocation)},
				{season: "summer", monthStart: time.August, monthEnd: time.October, effectiveDate: time.Date(2026, time.August, 1, 0, 0, 0, 0, ptLocation)},
				{season: "winter", monthStart: time.November, monthEnd: time.December, effectiveDate: time.Date(2026, time.November, 1, 0, 0, 0, 0, ptLocation)},
			}
		} else {
			subSeasons = []sdgeSubSeason{
				{season: "summer", monthStart: time.June, monthEnd: time.October, effectiveDate: time.Date(year, time.June, 1, 0, 0, 0, 0, ptLocation)},
				{season: "winter", monthStart: time.November, monthEnd: time.May, effectiveDate: time.Date(year, time.November, 1, 0, 0, 0, 0, ptLocation)},
			}
		}

		for _, ss := range subSeasons {
			season := ss.season
			monthStart := ss.monthStart
			monthEnd := ss.monthEnd
			effectiveDate := ss.effectiveDate
			pciaDollarsPerKWH := getSDGEPCIA(location, effectiveDate)

			baseDollarsPerKWH := func(touPeriod string) float64 {
				genDollarsPerKWH := getSDCPRate(plan, location, genRate, season, touPeriod) + pciaDollarsPerKWH
				if genRate == "sdge" {
					genDollarsPerKWH = getSDGEBundledEECC(plan, season, touPeriod, effectiveDate)
				}
				return getUDCRate(plan, season, touPeriod, effectiveDate) + genDollarsPerKWH - udcEmbeddedNBCDollarsPerKWH
			}

			if plan == "sdge_dr" {
				flatPeriod := touSimplifiedPeriod{
					Year:               year,
					MonthStart:         monthStart,
					MonthEnd:           monthEnd,
					OtherName:          "Tier 1",
					OtherDollarsPerKWH: baseDollarsPerKWH("Tier 1"),
					OtherDescription:   fmt.Sprintf("SDG&E %s %s Tier 1", plan, season),
				}
				periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{flatPeriod})...)
				continue
			}

			// 1. Holiday / Weekend Period (Weekends and observed holidays)
			holidayPeriod := touSimplifiedPeriod{
				Year: year, MonthStart: monthStart, MonthEnd: monthEnd, SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
						DollarsPerKWH: baseDollarsPerKWH("On-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend On-Peak", plan, season),
					},
					{
						Name:          "Off-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
						DollarsPerKWH: baseDollarsPerKWH("Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend Off-Peak", plan, season),
					},
					{
						Name:          "Super Off-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 14}},
						DollarsPerKWH: baseDollarsPerKWH("Super Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend Super Off-Peak", plan, season),
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: baseDollarsPerKWH("Off-Peak"),
				OtherDescription:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend Off-Peak", plan, season),
			}

			// 2. Regular Days (non-holidays)
			// Weekday Super Off-Peak: Midnight - 6:00 AM & 10:00 AM - 2:00 PM for all 3-period plans
			weekdaySuperOffPeakHours := []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 6}, {HourStart: 10, HourEnd: 14}}

			regularPeriod := touSimplifiedPeriod{
				Year: year, MonthStart: monthStart, MonthEnd: monthEnd, SpecificDates: holidays, SpecificDatesNot: true,
				HoursAndDays: []touSimplifiedHoursAndDays{
					// Weekday On-Peak (4:00 PM - 9:00 PM)
					{
						Name:          "On-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
						Weekday:       true,
						DollarsPerKWH: baseDollarsPerKWH("On-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekday On-Peak", plan, season),
					},
					// Weekday Super Off-Peak
					{
						Name:          "Super Off-Peak",
						Hours:         weekdaySuperOffPeakHours,
						Weekday:       true,
						DollarsPerKWH: baseDollarsPerKWH("Super Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekday Super Off-Peak", plan, season),
					},
					// Weekend On-Peak (4:00 PM - 9:00 PM)
					{
						Name:          "On-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
						Weekend:       true,
						DollarsPerKWH: baseDollarsPerKWH("On-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekend On-Peak", plan, season),
					},
					// Weekend Off-Peak (2:00 PM - 4:00 PM & 9:00 PM - 12:00 AM)
					{
						Name:          "Off-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
						Weekend:       true,
						DollarsPerKWH: baseDollarsPerKWH("Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekend Off-Peak", plan, season),
					},
					// Weekend Super Off-Peak (Midnight - 2:00 PM)
					{
						Name:          "Super Off-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 14}},
						Weekend:       true,
						DollarsPerKWH: baseDollarsPerKWH("Super Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekend Super Off-Peak", plan, season),
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: baseDollarsPerKWH("Off-Peak"),
				OtherDescription:   fmt.Sprintf("SDG&E %s %s Off-Peak", plan, season),
			}

			if plan == "sdge_tou_dr2" {
				holidayPeriod = touSimplifiedPeriod{
					Year:          year,
					MonthStart:    monthStart,
					MonthEnd:      monthEnd,
					SpecificDates: holidays,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: baseDollarsPerKWH("On-Peak"),
							Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend On-Peak", plan, season),
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: baseDollarsPerKWH("Off-Peak"),
					OtherDescription:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend Off-Peak", plan, season),
				}
				regularPeriod = touSimplifiedPeriod{
					Year:             year,
					MonthStart:       monthStart,
					MonthEnd:         monthEnd,
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					HoursAndDays: []touSimplifiedHoursAndDays{
						{
							Name:          "On-Peak",
							Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: baseDollarsPerKWH("On-Peak"),
							Description:   fmt.Sprintf("SDG&E %s %s Daily On-Peak", plan, season),
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: baseDollarsPerKWH("Off-Peak"),
					OtherDescription:   fmt.Sprintf("SDG&E %s %s Daily Off-Peak", plan, season),
				}
			}
			periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{holidayPeriod, regularPeriod})...)
		}
	}

	if options.NetMeteringScheme == "sbp" {
		accPlusDollarsPerKWH := sdgeACCPlusAdderDollarsPerKWH(options.ApplicationYear)
		for _, year := range years {
			holidays := getSDGEHolidays(year)
			for month := time.January; month <= time.December; month++ {
				monthStr := month.String()[:3]
				startMonth := time.Date(year, month, 1, 0, 0, 0, 0, ptLocation)
				endMonth := startMonth.AddDate(0, 1, 0)
				for hour := range 24 {
					weekdayKey := fmt.Sprintf("%s_Weekday_%d", monthStr, hour)
					weekdayDollarsPerKWH := sdgeNBTBaseDollarsPerKWH(year, options.ApplicationYear, weekdayKey) + accPlusDollarsPerKWH
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod: types.TimePeriod{
							Start:            startMonth,
							End:              endMonth,
							LocationPtr:      ptLocation,
							DaysOfTheWeek:    []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
							SpecificDates:    holidays,
							SpecificDatesNot: true,
							Hours:            []types.UtilityHourPeriod{{HourStart: hour, HourEnd: hour + 1}},
						},
						DollarsPerKWH:            weekdayDollarsPerKWH,
						SeparateGenerationCredit: true,
						Description:              fmt.Sprintf("SDG&E NBT Weekday Export Credit (Hour %d)", hour),
					})

					weekendKey := fmt.Sprintf("%s_Weekend_%d", monthStr, hour)
					weekendDollarsPerKWH := sdgeNBTBaseDollarsPerKWH(year, options.ApplicationYear, weekendKey) + accPlusDollarsPerKWH
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod: types.TimePeriod{
							Start:            startMonth,
							End:              endMonth,
							LocationPtr:      ptLocation,
							DaysOfTheWeek:    []time.Weekday{time.Saturday, time.Sunday},
							SpecificDatesNot: false,
							Hours:            []types.UtilityHourPeriod{{HourStart: hour, HourEnd: hour + 1}},
						},
						DollarsPerKWH:            weekendDollarsPerKWH,
						SeparateGenerationCredit: true,
						Description:              fmt.Sprintf("SDG&E NBT Weekend Export Credit (Hour %d)", hour),
					})
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod: types.TimePeriod{
							Start:            startMonth,
							End:              endMonth,
							LocationPtr:      ptLocation,
							DaysOfTheWeek:    []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
							SpecificDates:    holidays,
							SpecificDatesNot: false,
							Hours:            []types.UtilityHourPeriod{{HourStart: hour, HourEnd: hour + 1}},
						},
						DollarsPerKWH:            weekendDollarsPerKWH,
						SeparateGenerationCredit: true,
						Description:              fmt.Sprintf("SDG&E NBT Holiday Export Credit (Hour %d)", hour),
					})
				}
			}
		}
	}
	periods = append(periods, types.UtilityFeesPeriod{
		TimePeriod:     types.TimePeriod{LocationPtr: ptLocation},
		DollarsPerKWH:  nemSTNBCDollarsPerKWH,
		GridAdditional: true,
		Description:    "SDG&E Non-Bypassable Charges",
	})
	return periods
}

// getSDGEPCIA returns the residential Power Charge Indifference Adjustment ($/kWh) for SDCP communities:
// - san_diego (2021 Vintage): $0.03557 (1/1/26), $0.03564 (4/1/26-6/1/26), $0.03348 (8/1/26+)
// - unincorporated (2022 Vintage): $0.02999 (1/1/26), $0.03005 (4/1/26-6/1/26), $0.02789 (8/1/26+)
func getSDGEPCIA(location string, t time.Time) float64 {
	august12026 := time.Date(2026, time.August, 1, 0, 0, 0, 0, ptLocation)
	april12026 := time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation)

	if location == "unincorporated" {
		if !t.Before(august12026) {
			return 0.02789
		}
		if !t.Before(april12026) {
			return 0.03005
		}
		return 0.02999
	}

	if !t.Before(august12026) {
		return 0.03348
	}
	if !t.Before(april12026) {
		return 0.03564
	}
	return 0.03557
}

func getUDCRate(plan, season, period string, t time.Time) float64 {
	august12026 := time.Date(2026, time.August, 1, 0, 0, 0, 0, ptLocation)
	june12026 := time.Date(2026, time.June, 1, 0, 0, 0, 0, ptLocation)
	april12026 := time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation)

	if !t.Before(august12026) {
		const tracCreditAug2026DollarsPerKWH = 0.10702
		switch plan {
		case "sdge_dr":
			return 0.21900
		case "sdge_ev_tou":
			switch period {
			case "On-Peak", "Off-Peak":
				return 0.37290
			case "Super Off-Peak":
				return 0.21887
			}
		case "sdge_ev_tou_2":
			switch period {
			case "On-Peak", "Off-Peak":
				return 0.29905
			case "Super Off-Peak":
				return 0.16139
			}
		case "sdge_ev_tou_5":
			switch period {
			case "On-Peak", "Off-Peak":
				return 0.31218
			case "Super Off-Peak":
				return 0.04114
			}
		case "sdge_tou_dr", "sdge_tou_dr1":
			switch period {
			case "On-Peak", "Off-Peak", "Super Off-Peak":
				return 0.32601 - tracCreditAug2026DollarsPerKWH
			}
		case "sdge_tou_dr2":
			switch period {
			case "On-Peak":
				if season == "summer" {
					return 0.33063 - tracCreditAug2026DollarsPerKWH
				}
				return 0.32601 - tracCreditAug2026DollarsPerKWH
			case "Off-Peak":
				if season == "summer" {
					return 0.32397 - tracCreditAug2026DollarsPerKWH
				}
				return 0.32601 - tracCreditAug2026DollarsPerKWH
			}
		case "sdge_tou_elec":
			switch period {
			case "On-Peak", "Off-Peak", "Super Off-Peak":
				return 0.24970
			}
		case "sdge_dr_ses":
			switch period {
			case "On-Peak", "Off-Peak", "Super Off-Peak":
				return 0.25957
			}
		}
		return 0.0
	}

	if !t.Before(june12026) {
		const tracCreditJun2026DollarsPerKWH = 0.10663
		switch plan {
		case "sdge_dr":
			return 0.22285
		case "sdge_ev_tou":
			switch period {
			case "On-Peak", "Off-Peak":
				return 0.37721
			case "Super Off-Peak":
				return 0.22056
			}
		case "sdge_ev_tou_2":
			switch period {
			case "On-Peak", "Off-Peak":
				return 0.30372
			case "Super Off-Peak":
				return 0.16275
			}
		case "sdge_ev_tou_5":
			switch period {
			case "On-Peak", "Off-Peak":
				return 0.31711
			case "Super Off-Peak":
				return 0.04114
			}
		case "sdge_tou_dr", "sdge_tou_dr1":
			switch period {
			case "On-Peak", "Off-Peak", "Super Off-Peak":
				return 0.32948 - tracCreditJun2026DollarsPerKWH
			}
		case "sdge_tou_dr2":
			switch period {
			case "On-Peak":
				if season == "summer" {
					return 0.33396 - tracCreditJun2026DollarsPerKWH
				}
				return 0.32948 - tracCreditJun2026DollarsPerKWH
			case "Off-Peak":
				if season == "summer" {
					return 0.32750 - tracCreditJun2026DollarsPerKWH
				}
				return 0.32948 - tracCreditJun2026DollarsPerKWH
			}
		case "sdge_tou_elec":
			switch period {
			case "On-Peak", "Off-Peak", "Super Off-Peak":
				return 0.25317
			}
		case "sdge_dr_ses":
			switch period {
			case "On-Peak", "Off-Peak", "Super Off-Peak":
				return 0.26328
			}
		}
		return 0.0
	}

	if !t.Before(april12026) {
		const tracCreditApr2026DollarsPerKWH = 0.10892
		switch plan {
		case "sdge_dr":
			return 0.23169
		case "sdge_ev_tou":
			switch period {
			case "On-Peak", "Off-Peak":
				return 0.38692
			case "Super Off-Peak":
				return 0.23027
			}
		case "sdge_ev_tou_2":
			switch period {
			case "On-Peak", "Off-Peak":
				return 0.31343
			case "Super Off-Peak":
				return 0.17246
			}
		case "sdge_ev_tou_5":
			switch period {
			case "On-Peak", "Off-Peak":
				return 0.32682
			case "Super Off-Peak":
				return 0.04114
			}
		case "sdge_tou_dr", "sdge_tou_dr1":
			switch period {
			case "On-Peak", "Off-Peak", "Super Off-Peak":
				return 0.34061 - tracCreditApr2026DollarsPerKWH
			}
		case "sdge_tou_dr2":
			switch period {
			case "On-Peak":
				if season == "summer" {
					return 0.34510 - tracCreditApr2026DollarsPerKWH
				}
				return 0.34061 - tracCreditApr2026DollarsPerKWH
			case "Off-Peak":
				if season == "summer" {
					return 0.33863 - tracCreditApr2026DollarsPerKWH
				}
				return 0.34061 - tracCreditApr2026DollarsPerKWH
			}
		case "sdge_tou_elec":
			switch period {
			case "On-Peak", "Off-Peak", "Super Off-Peak":
				return 0.26288
			}
		case "sdge_dr_ses":
			switch period {
			case "On-Peak", "Off-Peak", "Super Off-Peak":
				return 0.27299
			}
		}
		return 0.0
	}

	const tracCreditJan2026DollarsPerKWH = 0.10905
	switch plan {
	case "sdge_dr":
		return 0.23197
	case "sdge_ev_tou":
		switch period {
		case "On-Peak", "Off-Peak":
			return 0.38124
		case "Super Off-Peak":
			return 0.22804
		}
	case "sdge_ev_tou_2":
		switch period {
		case "On-Peak", "Off-Peak":
			return 0.30766
		case "Super Off-Peak":
			return 0.17078
		}
	case "sdge_ev_tou_5":
		switch period {
		case "On-Peak", "Off-Peak":
			return 0.32322
		case "Super Off-Peak":
			return 0.03676
		}
	case "sdge_tou_dr", "sdge_tou_dr1":
		switch period {
		case "On-Peak", "Off-Peak", "Super Off-Peak":
			return 0.34101 - tracCreditJan2026DollarsPerKWH
		}
	case "sdge_tou_dr2":
		switch period {
		case "On-Peak":
			if season == "summer" {
				return 0.34550 - tracCreditJan2026DollarsPerKWH
			}
			return 0.34101 - tracCreditJan2026DollarsPerKWH
		case "Off-Peak":
			if season == "summer" {
				return 0.33903 - tracCreditJan2026DollarsPerKWH
			}
			return 0.34101 - tracCreditJan2026DollarsPerKWH
		}
	case "sdge_tou_elec":
		switch period {
		case "On-Peak", "Off-Peak", "Super Off-Peak":
			return 0.25858
		}
	case "sdge_dr_ses":
		switch period {
		case "On-Peak", "Off-Peak", "Super Off-Peak":
			return 0.26840
		}
	}
	return 0.0
}

func getSDGEBundledEECC(plan, season, period string, t time.Time) float64 {
	august12026 := time.Date(2026, time.August, 1, 0, 0, 0, 0, ptLocation)
	april12026 := time.Date(2026, time.April, 1, 0, 0, 0, 0, ptLocation)

	if !t.Before(august12026) {
		switch plan {
		case "sdge_dr":
			return 0.18808
		case "sdge_ev_tou", "sdge_ev_tou_2", "sdge_ev_tou_5", "sdge_dr_ses":
			if season == "summer" {
				switch period {
				case "On-Peak":
					return 0.48396
				case "Off-Peak":
					return 0.17818
				case "Super Off-Peak":
					return 0.08385
				}
			} else {
				switch period {
				case "On-Peak":
					return 0.20574
				case "Off-Peak":
					return 0.14757
				case "Super Off-Peak":
					return 0.07627
				}
			}
		case "sdge_tou_elec":
			if season == "summer" {
				switch period {
				case "On-Peak":
					return 0.47028
				case "Off-Peak":
					return 0.13324
				case "Super Off-Peak":
					return 0.08889
				}
			} else {
				switch period {
				case "On-Peak":
					return 0.25023
				case "Off-Peak":
					return 0.12118
				case "Super Off-Peak":
					return 0.08085
				}
			}
		case "sdge_tou_dr":
			if season == "summer" {
				switch period {
				case "On-Peak":
					return 0.23565
				case "Off-Peak":
					return 0.17504
				case "Super Off-Peak":
					return 0.11792
				}
			} else {
				switch period {
				case "On-Peak":
					return 0.28256
				case "Off-Peak":
					return 0.19852
				case "Super Off-Peak":
					return 0.10519
				}
			}
		case "sdge_tou_dr1":
			if season == "summer" {
				switch period {
				case "On-Peak":
					return 0.35943
				case "Off-Peak":
					return 0.13229
				case "Super Off-Peak":
					return 0.04241
				}
			} else {
				switch period {
				case "On-Peak":
					return 0.28279
				case "Off-Peak":
					return 0.19868
				case "Super Off-Peak":
					return 0.10527
				}
			}
		case "sdge_tou_dr2":
			if season == "summer" {
				switch period {
				case "On-Peak":
					return 0.35943
				case "Off-Peak":
					return 0.08678
				}
			} else {
				switch period {
				case "On-Peak":
					return 0.28279
				case "Off-Peak":
					return 0.14180
				}
			}
		}
		return 0.0
	}

	if !t.Before(april12026) {
		switch plan {
		case "sdge_dr":
			return 0.18274
		case "sdge_ev_tou", "sdge_ev_tou_2", "sdge_ev_tou_5", "sdge_dr_ses":
			if season == "summer" {
				switch period {
				case "On-Peak":
					return 0.47019
				case "Off-Peak":
					return 0.17311
				case "Super Off-Peak":
					return 0.08147
				}
			} else {
				switch period {
				case "On-Peak":
					return 0.19990
				case "Off-Peak":
					return 0.14337
				case "Super Off-Peak":
					return 0.07410
				}
			}
		case "sdge_tou_dr":
			if season == "summer" {
				switch period {
				case "On-Peak":
					return 0.22895
				case "Off-Peak":
					return 0.17007
				case "Super Off-Peak":
					return 0.11457
				}
			} else {
				switch period {
				case "On-Peak":
					return 0.27453
				case "Off-Peak":
					return 0.19288
				case "Super Off-Peak":
					return 0.10220
				}
			}
		case "sdge_tou_dr1":
			if season == "summer" {
				switch period {
				case "On-Peak":
					return 0.34920
				case "Off-Peak":
					return 0.12853
				case "Super Off-Peak":
					return 0.04121
				}
			} else {
				switch period {
				case "On-Peak":
					return 0.27475
				case "Off-Peak":
					return 0.19304
				case "Super Off-Peak":
					return 0.10228
				}
			}
		case "sdge_tou_dr2":
			if season == "summer" {
				switch period {
				case "On-Peak":
					return 0.34920
				case "Off-Peak":
					return 0.08432
				}
			} else {
				switch period {
				case "On-Peak":
					return 0.27475
				case "Off-Peak":
					return 0.13777
				}
			}
		case "sdge_tou_elec":
			if season == "summer" {
				switch period {
				case "On-Peak":
					return 0.45690
				case "Off-Peak":
					return 0.12945
				case "Super Off-Peak":
					return 0.08637
				}
			} else {
				switch period {
				case "On-Peak":
					return 0.24311
				case "Off-Peak":
					return 0.11774
				case "Super Off-Peak":
					return 0.07856
				}
			}
		}
		return 0.0
	}

	switch plan {
	case "sdge_dr":
		return 0.18296
	case "sdge_ev_tou", "sdge_ev_tou_2", "sdge_ev_tou_5", "sdge_dr_ses":
		if season == "summer" {
			switch period {
			case "On-Peak":
				return 0.47075
			case "Off-Peak":
				return 0.17332
			case "Super Off-Peak":
				return 0.08157
			}
		} else {
			switch period {
			case "On-Peak":
				return 0.20013
			case "Off-Peak":
				return 0.14354
			case "Super Off-Peak":
				return 0.07419
			}
		}
	case "sdge_tou_dr":
		if season == "summer" {
			switch period {
			case "On-Peak":
				return 0.22922
			case "Off-Peak":
				return 0.17027
			case "Super Off-Peak":
				return 0.11471
			}
		} else {
			switch period {
			case "On-Peak":
				return 0.27485
			case "Off-Peak":
				return 0.19311
			case "Super Off-Peak":
				return 0.10232
			}
		}
	case "sdge_tou_dr1":
		if season == "summer" {
			switch period {
			case "On-Peak":
				return 0.34962
			case "Off-Peak":
				return 0.12868
			case "Super Off-Peak":
				return 0.04126
			}
		} else {
			switch period {
			case "On-Peak":
				return 0.27508
			case "Off-Peak":
				return 0.19327
			case "Super Off-Peak":
				return 0.10241
			}
		}
	case "sdge_tou_dr2":
		if season == "summer" {
			switch period {
			case "On-Peak":
				return 0.34962
			case "Off-Peak":
				return 0.08442
			}
		} else {
			switch period {
			case "On-Peak":
				return 0.27508
			case "Off-Peak":
				return 0.13793
			}
		}
	case "sdge_tou_elec":
		if season == "summer" {
			switch period {
			case "On-Peak":
				return 0.45745
			case "Off-Peak":
				return 0.12961
			case "Super Off-Peak":
				return 0.08647
			}
		} else {
			switch period {
			case "On-Peak":
				return 0.24340
			case "Off-Peak":
				return 0.11788
			case "Super Off-Peak":
				return 0.07865
			}
		}
	}
	return 0.0
}

// getSDCPRate returns the commodity generation rate for SDCP unbundled service.
func getSDCPRate(plan, location, tier, season, period string) float64 {
	// Map options to standard SDCP plan name
	var sdcpPlan string
	switch plan {
	case "sdge_dr":
		sdcpPlan = "DR"
	case "sdge_ev_tou":
		sdcpPlan = "EV-TOU"
	case "sdge_ev_tou_2":
		sdcpPlan = "EV-TOU-2"
	case "sdge_ev_tou_5":
		sdcpPlan = "EV-TOU-5"
	case "sdge_tou_dr":
		sdcpPlan = "TOU-DR"
	case "sdge_tou_dr1":
		sdcpPlan = "TOU-DR-1"
	case "sdge_tou_dr2":
		sdcpPlan = "TOU-DR-2"
	case "sdge_tou_elec":
		sdcpPlan = "TOU-ELEC"
	case "sdge_dr_ses":
		sdcpPlan = "DR-SES"
	default:
		return 0.0
	}

	rates2021v := map[string]map[string]map[string]map[string]float64{
		"DR": {
			"powerOn": {
				"summer": {"Tier 1": 0.13780},
				"winter": {"Tier 1": 0.13780},
			},
			"powerBase": {
				"summer": {"Tier 1": 0.12684},
				"winter": {"Tier 1": 0.12684},
			},
		},
		"EV-TOU": {
			"powerOn": {
				"summer": {"On-Peak": 0.41063, "Off-Peak": 0.12866, "Super Off-Peak": 0.04168},
				"winter": {"On-Peak": 0.15409, "Off-Peak": 0.10044, "Super Off-Peak": 0.03469},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38242, "Off-Peak": 0.11828, "Super Off-Peak": 0.03680},
				"winter": {"On-Peak": 0.14210, "Off-Peak": 0.09183, "Super Off-Peak": 0.03024},
			},
		},
		"DR-SES": {
			"powerOn": {
				"summer": {"On-Peak": 0.41063, "Off-Peak": 0.12866, "Super Off-Peak": 0.04168},
				"winter": {"On-Peak": 0.15409, "Off-Peak": 0.10044, "Super Off-Peak": 0.03469},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38242, "Off-Peak": 0.11828, "Super Off-Peak": 0.03680},
				"winter": {"On-Peak": 0.14210, "Off-Peak": 0.09183, "Super Off-Peak": 0.03024},
			},
		},
		"EV-TOU-2": {
			"powerOn": {
				"summer": {"On-Peak": 0.41063, "Off-Peak": 0.12866, "Super Off-Peak": 0.04168},
				"winter": {"On-Peak": 0.15409, "Off-Peak": 0.10044, "Super Off-Peak": 0.03469},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38242, "Off-Peak": 0.11828, "Super Off-Peak": 0.03680},
				"winter": {"On-Peak": 0.14210, "Off-Peak": 0.09183, "Super Off-Peak": 0.03024},
			},
		},
		"EV-TOU-5": {
			"powerOn": {
				"summer": {"On-Peak": 0.41063, "Off-Peak": 0.12866, "Super Off-Peak": 0.04168},
				"winter": {"On-Peak": 0.15409, "Off-Peak": 0.10044, "Super Off-Peak": 0.03469},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38242, "Off-Peak": 0.11828, "Super Off-Peak": 0.03680},
				"winter": {"On-Peak": 0.14210, "Off-Peak": 0.09183, "Super Off-Peak": 0.03024},
			},
		},
		"TOU-DR": {
			"powerOn": {
				"summer": {"On-Peak": 0.18166, "Off-Peak": 0.12578, "Super Off-Peak": 0.07310},
				"winter": {"On-Peak": 0.22492, "Off-Peak": 0.14743, "Super Off-Peak": 0.06136},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.16792, "Off-Peak": 0.11557, "Super Off-Peak": 0.06623},
				"winter": {"On-Peak": 0.20845, "Off-Peak": 0.13585, "Super Off-Peak": 0.05523},
			},
		},
		"TOU-DR-1": {
			"powerOn": {
				"summer": {"On-Peak": 0.29579, "Off-Peak": 0.08635, "Super Off-Peak": 0.01000},
				"winter": {"On-Peak": 0.22513, "Off-Peak": 0.14758, "Super Off-Peak": 0.06144},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.27484, "Off-Peak": 0.07864, "Super Off-Peak": 0.01000},
				"winter": {"On-Peak": 0.20865, "Off-Peak": 0.13600, "Super Off-Peak": 0.05530},
			},
		},
		"TOU-DR-2": {
			"powerOn": {
				"summer": {"On-Peak": 0.29579, "Off-Peak": 0.04439},
				"winter": {"On-Peak": 0.22513, "Off-Peak": 0.09512},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.27484, "Off-Peak": 0.03933},
				"winter": {"On-Peak": 0.20865, "Off-Peak": 0.08685},
			},
		},
		"TOU-ELEC": {
			"powerOn": {
				"summer": {"On-Peak": 0.39801, "Off-Peak": 0.08722, "Super Off-Peak": 0.04634},
				"winter": {"On-Peak": 0.19510, "Off-Peak": 0.07611, "Super Off-Peak": 0.03892},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.37060, "Off-Peak": 0.07946, "Super Off-Peak": 0.04115},
				"winter": {"On-Peak": 0.18051, "Off-Peak": 0.06904, "Super Off-Peak": 0.03421},
			},
		},
	}

	rates2022v := map[string]map[string]map[string]map[string]float64{
		"DR": {
			"powerOn": {
				"summer": {"Tier 1": 0.14339},
				"winter": {"Tier 1": 0.14339},
			},
			"powerBase": {
				"summer": {"Tier 1": 0.13243},
				"winter": {"Tier 1": 0.13243},
			},
		},
		"EV-TOU": {
			"powerOn": {
				"summer": {"On-Peak": 0.41622, "Off-Peak": 0.13425, "Super Off-Peak": 0.04727},
				"winter": {"On-Peak": 0.15968, "Off-Peak": 0.10603, "Super Off-Peak": 0.04028},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38801, "Off-Peak": 0.12387, "Super Off-Peak": 0.04239},
				"winter": {"On-Peak": 0.14769, "Off-Peak": 0.09742, "Super Off-Peak": 0.03583},
			},
		},
		"DR-SES": {
			"powerOn": {
				"summer": {"On-Peak": 0.41622, "Off-Peak": 0.13425, "Super Off-Peak": 0.04727},
				"winter": {"On-Peak": 0.15968, "Off-Peak": 0.10603, "Super Off-Peak": 0.04028},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38801, "Off-Peak": 0.12387, "Super Off-Peak": 0.04239},
				"winter": {"On-Peak": 0.14769, "Off-Peak": 0.09742, "Super Off-Peak": 0.03583},
			},
		},
		"EV-TOU-2": {
			"powerOn": {
				"summer": {"On-Peak": 0.41622, "Off-Peak": 0.13425, "Super Off-Peak": 0.04727},
				"winter": {"On-Peak": 0.15968, "Off-Peak": 0.10603, "Super Off-Peak": 0.04028},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38801, "Off-Peak": 0.12387, "Super Off-Peak": 0.04239},
				"winter": {"On-Peak": 0.14769, "Off-Peak": 0.09742, "Super Off-Peak": 0.03583},
			},
		},
		"EV-TOU-5": {
			"powerOn": {
				"summer": {"On-Peak": 0.41622, "Off-Peak": 0.13425, "Super Off-Peak": 0.04727},
				"winter": {"On-Peak": 0.15968, "Off-Peak": 0.10603, "Super Off-Peak": 0.04028},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.38801, "Off-Peak": 0.12387, "Super Off-Peak": 0.04239},
				"winter": {"On-Peak": 0.14769, "Off-Peak": 0.09742, "Super Off-Peak": 0.03583},
			},
		},
		"TOU-DR": {
			"powerOn": {
				"summer": {"On-Peak": 0.18725, "Off-Peak": 0.13137, "Super Off-Peak": 0.07869},
				"winter": {"On-Peak": 0.23051, "Off-Peak": 0.15302, "Super Off-Peak": 0.06695},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.17351, "Off-Peak": 0.12116, "Super Off-Peak": 0.07182},
				"winter": {"On-Peak": 0.21404, "Off-Peak": 0.14144, "Super Off-Peak": 0.06082},
			},
		},
		"TOU-DR-1": {
			"powerOn": {
				"summer": {"On-Peak": 0.30138, "Off-Peak": 0.09194, "Super Off-Peak": 0.01000},
				"winter": {"On-Peak": 0.23072, "Off-Peak": 0.15317, "Super Off-Peak": 0.06703},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.28043, "Off-Peak": 0.08423, "Super Off-Peak": 0.01000},
				"winter": {"On-Peak": 0.21424, "Off-Peak": 0.14159, "Super Off-Peak": 0.06089},
			},
		},
		"TOU-DR-2": {
			"powerOn": {
				"summer": {"On-Peak": 0.30138, "Off-Peak": 0.04998},
				"winter": {"On-Peak": 0.23072, "Off-Peak": 0.10071},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.28043, "Off-Peak": 0.04492},
				"winter": {"On-Peak": 0.21424, "Off-Peak": 0.09244},
			},
		},
		"TOU-ELEC": {
			"powerOn": {
				"summer": {"On-Peak": 0.40360, "Off-Peak": 0.09281, "Super Off-Peak": 0.05193},
				"winter": {"On-Peak": 0.20069, "Off-Peak": 0.08170, "Super Off-Peak": 0.04451},
			},
			"powerBase": {
				"summer": {"On-Peak": 0.37619, "Off-Peak": 0.08505, "Super Off-Peak": 0.04674},
				"winter": {"On-Peak": 0.18610, "Off-Peak": 0.07463, "Super Off-Peak": 0.03980},
			},
		},
	}

	rates := rates2021v
	if location == "unincorporated" {
		rates = rates2022v
	}

	tierKey := "powerOn"
	if tier == "sdcp_power_base" {
		tierKey = "powerBase"
	}

	premiumDollarsPerKWH := 0.0
	if tier == "sdcp_power_100" {
		premiumDollarsPerKWH = 0.01
	}

	if planMap, ok := rates[sdcpPlan]; ok {
		if tierMap, ok := planMap[tierKey]; ok {
			if seasonMap, ok := tierMap[season]; ok {
				if val, ok := seasonMap[period]; ok {
					return val + premiumDollarsPerKWH
				}
			}
		}
	}
	return 0.0
}

// sdgeUtilityInfo returns the metadata and rates for SDG&E.
func sdgeUtilityInfo() types.UtilityProviderInfo {
	sdgeOptions := []types.UtilityRateOption{
		{
			Field:       "generationRate",
			Name:        "Generation Rate",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your generation provider and rate plan.",
			Choices: []types.UtilityOptionChoice{
				{Value: "sdge", Name: "SDG&E Bundled"},
				{Value: "sdcp_power_base", Name: "SDCP PowerBase"},
				{Value: "sdcp_power_on", Name: "SDCP PowerOn"},
				{Value: "sdcp_power_100", Name: "SDCP Power100"},
			},
			Default: "sdcp_power_on",
		},
		{
			Field:       "location",
			Name:        "Location / Community",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your location context for Community Power rates.",
			Choices: []types.UtilityOptionChoice{
				{Value: "san_diego", Name: "Chula Vista, Encinitas, Imperial Beach, La Mesa and San Diego"},
				{Value: "unincorporated", Name: "National City and Unincorporated County of San Diego"},
			},
			Default: "san_diego",
			DependsOn: map[string][]any{
				"generationRate": {"sdcp_power_base", "sdcp_power_on", "sdcp_power_100"},
			},
		},
		{
			Field:       "netMeteringScheme",
			Name:        "Net Metering / Export Scheme",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your net metering or solar billing plan program.",
			Choices: []types.UtilityOptionChoice{
				{Value: "net", Name: "NEM 1.0 (installs before June 2016)"},
				{Value: "nem2", Name: "NEM 2.0 (installs between June 2016 and April 15, 2023)"},
				{Value: "sbp", Name: "Solar Billing Plan (NEM 3.0) (installs after April 15, 2023)"},
			},
			Default: "nem2",
		},
		{
			Field:       "applicationYear",
			Name:        "Interconnection Application Year (SBP Cohort)",
			Type:        types.UtilityOptionTypeSelect,
			Description: "Select your Solar Billing Plan (Schedule NBT) interconnection cohort for export pricing.",
			Choices: []types.UtilityOptionChoice{
				{Value: "2023", Name: "2023 (NBT23, $0.000/kWh ACC Plus)"},
				{Value: "2024", Name: "2024 (NBT24, $0.000/kWh ACC Plus)"},
				{Value: "2025", Name: "2025 (NBT25, $0.000/kWh ACC Plus)"},
				{Value: "2026", Name: "2026 (NBT26, $0.000/kWh ACC Plus)"},
				{Value: "2027", Name: "2027 (NBT27, $0.000/kWh ACC Plus)"},
				{Value: "non_locked", Name: "Non-Locked In (Post-9-Year / NBT00, $0.000/kWh ACC Plus)"},
			},
			Default: "2026",
			DependsOn: map[string][]any{
				"netMeteringScheme": {"sbp"},
			},
		},
	}

	return types.UtilityProviderInfo{
		ID:   "sdge",
		Name: "San Diego Gas & Electric (SDG&E)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "sdge_dr",
				Name:    "Schedule DR (Residential Service)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_dr", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_ev_tou",
				Name:    "Schedule EV-TOU (EV Meter Only)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_ev_tou", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_ev_tou_2",
				Name:    "Schedule EV-TOU-2 (Whole House EV)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_ev_tou_2", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_ev_tou_5",
				Name:    "Schedule EV-TOU-5 (Whole House EV with Service Fee)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_ev_tou_5", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_tou_dr",
				Name:    "Schedule TOU-DR (Residential TOU)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_tou_dr", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_tou_dr1",
				Name:    "Schedule TOU-DR1 (Standard Residential TOU)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_tou_dr1", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_tou_dr2",
				Name:    "Schedule TOU-DR2 (2-Period TOU)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_tou_dr2", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_tou_elec",
				Name:    "Schedule TOU-ELEC (EV/Storage/Heat Pump)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_tou_elec", opts, []int{2026, 2027}), nil
				},
			},
			{
				ID:      "sdge_dr_ses",
				Name:    "Schedule DR-SES (Domestic TOU for Solar Systems)",
				Options: sdgeOptions,
				GetFees: func(opts types.UtilityRateOptions) ([]types.UtilityFeesPeriod, error) {
					return sdgePeriods("sdge_dr_ses", opts, []int{2026, 2027}), nil
				},
			},
		},
	}
}
