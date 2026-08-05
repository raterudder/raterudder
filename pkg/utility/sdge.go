package utility

import (
	"fmt"
	"time"

	"github.com/raterudder/raterudder/pkg/types"
)

// sdgeNBTData contains Avoided Cost Calculator (ACC) NBT26 export rates ($/kWh)
// for 2026 and 2027, representing the sum of Generation and Delivery credit rates,
// grouped by Month_DayType_Hour.
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
		"Nov_Weekend_1":  0.083575,
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
		"Nov_Weekend_1":  0.085349,
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

// getSDGEHolidays calculates designated holidays for SDG&E.
// The following 8 holidays are observed:
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

// getSDGENBTExportRate returns the export rate for SDG&E Net Billing Tariff (NEM 3.0)
// for a given time.
func getSDGENBTExportRate(t time.Time) float64 {
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
		// SDG&E holidays
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

	key := fmt.Sprintf("%s_%s_%d", monthStr, dayType, t.Hour())

	if yearMap, ok := sdgeNBTData[year]; ok {
		if val, ok := yearMap[key]; ok {
			return val
		}
	}

	// Fallback to 2026 if not found
	if val, ok := sdgeNBTData[2026][key]; ok {
		return val
	}
	return 0.05 // safe fallback
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

	const nbc = 0.00591

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
				{season: "winter", monthStart: time.January, monthEnd: time.May, effectiveDate: time.Date(2026, time.January, 1, 0, 0, 0, 0, ptLocation)},
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
			pcia := getSDGEPCIA(effectiveDate)

			getGenRate := func(touPeriod string) float64 {
				if genRate == "sdge" {
					return getSDGEBundledEECC(plan, season, touPeriod, effectiveDate)
				}
				return getSDCPRate(plan, location, genRate, season, touPeriod) + pcia
			}

			// 1. Holiday / Weekend Period (Weekends and observed holidays)
			holidayPeriod := touSimplifiedPeriod{
				Year: year, MonthStart: monthStart, MonthEnd: monthEnd, SpecificDates: holidays,
				HoursAndDays: []touSimplifiedHoursAndDays{
					{
						Name:          "On-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
						DollarsPerKWH: getUDCRate(plan, season, "On-Peak", effectiveDate) + getGenRate("On-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend On-Peak", plan, season),
					},
					{
						Name:          "Off-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
						DollarsPerKWH: getUDCRate(plan, season, "Off-Peak", effectiveDate) + getGenRate("Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend Off-Peak", plan, season),
					},
					{
						Name:          "Super Off-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 14}},
						DollarsPerKWH: getUDCRate(plan, season, "Super Off-Peak", effectiveDate) + getGenRate("Super Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend Super Off-Peak", plan, season),
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: getUDCRate(plan, season, "Off-Peak", effectiveDate) + getGenRate("Off-Peak"),
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
						DollarsPerKWH: getUDCRate(plan, season, "On-Peak", effectiveDate) + getGenRate("On-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekday On-Peak", plan, season),
					},
					// Weekday Super Off-Peak
					{
						Name:          "Super Off-Peak",
						Hours:         weekdaySuperOffPeakHours,
						Weekday:       true,
						DollarsPerKWH: getUDCRate(plan, season, "Super Off-Peak", effectiveDate) + getGenRate("Super Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekday Super Off-Peak", plan, season),
					},
					// Weekend On-Peak (4:00 PM - 9:00 PM)
					{
						Name:          "On-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
						Weekend:       true,
						DollarsPerKWH: getUDCRate(plan, season, "On-Peak", effectiveDate) + getGenRate("On-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekend On-Peak", plan, season),
					},
					// Weekend Off-Peak (2:00 PM - 4:00 PM & 9:00 PM - 12:00 AM)
					{
						Name:          "Off-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 14, HourEnd: 16}, {HourStart: 21, HourEnd: 24}},
						Weekend:       true,
						DollarsPerKWH: getUDCRate(plan, season, "Off-Peak", effectiveDate) + getGenRate("Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekend Off-Peak", plan, season),
					},
					// Weekend Super Off-Peak (Midnight - 2:00 PM)
					{
						Name:          "Super Off-Peak",
						Hours:         []types.UtilityHourPeriod{{HourStart: 0, HourEnd: 14}},
						Weekend:       true,
						DollarsPerKWH: getUDCRate(plan, season, "Super Off-Peak", effectiveDate) + getGenRate("Super Off-Peak"),
						Description:   fmt.Sprintf("SDG&E %s %s Weekend Super Off-Peak", plan, season),
					},
				},
				OtherName:          "Off-Peak",
				OtherDollarsPerKWH: getUDCRate(plan, season, "Off-Peak", effectiveDate) + getGenRate("Off-Peak"),
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
							Name: "On-Peak", Hours: []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: getUDCRate(plan, season, "On-Peak", effectiveDate) + getGenRate("On-Peak"),
							Description:   fmt.Sprintf("SDG&E %s %s Holiday/Weekend On-Peak", plan, season),
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: getUDCRate(plan, season, "Off-Peak", effectiveDate) + getGenRate("Off-Peak"),
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
							Name: "On-Peak", Hours: []types.UtilityHourPeriod{{HourStart: 16, HourEnd: 21}},
							DollarsPerKWH: getUDCRate(plan, season, "On-Peak", effectiveDate) + getGenRate("On-Peak"),
							Description:   fmt.Sprintf("SDG&E %s %s Daily On-Peak", plan, season),
						},
					},
					OtherName:          "Off-Peak",
					OtherDollarsPerKWH: getUDCRate(plan, season, "Off-Peak", effectiveDate) + getGenRate("Off-Peak"),
					OtherDescription:   fmt.Sprintf("SDG&E %s %s Daily Off-Peak", plan, season),
				}
			}
			periods = append(periods, buildPeriods(ptLocation, []touSimplifiedPeriod{holidayPeriod, regularPeriod})...)
		}
	}

	if options.NetMeteringScheme == "sbp" {
		for _, year := range years {
			holidays := getSDGEHolidays(year)
			for month := time.January; month <= time.December; month++ {
				startMonth := time.Date(year, month, 1, 0, 0, 0, 0, ptLocation)
				endMonth := startMonth.AddDate(0, 1, 0)
				for hour := 0; hour < 24; hour++ {
					targetWeekday := startMonth.Add(time.Duration(hour) * time.Hour)
					for targetWeekday.Weekday() == time.Saturday || targetWeekday.Weekday() == time.Sunday {
						targetWeekday = targetWeekday.AddDate(0, 0, 1)
					}
					weekdayVal := getSDGENBTExportRate(targetWeekday)
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
						DollarsPerKWH:            weekdayVal,
						SeparateGenerationCredit: true,
						Description:              fmt.Sprintf("SDG&E NBT Weekday Export Credit (Hour %d)", hour),
					})
					targetWeekend := startMonth.Add(time.Duration(hour) * time.Hour)
					for targetWeekend.Weekday() != time.Saturday && targetWeekend.Weekday() != time.Sunday {
						targetWeekend = targetWeekend.AddDate(0, 0, 1)
					}
					weekendVal := getSDGENBTExportRate(targetWeekend)
					periods = append(periods, types.UtilityFeesPeriod{
						TimePeriod: types.TimePeriod{
							Start:            startMonth,
							End:              endMonth,
							LocationPtr:      ptLocation,
							DaysOfTheWeek:    []time.Weekday{time.Saturday, time.Sunday},
							SpecificDatesNot: false,
							Hours:            []types.UtilityHourPeriod{{HourStart: hour, HourEnd: hour + 1}},
						},
						DollarsPerKWH:            weekendVal,
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
						DollarsPerKWH:            weekendVal,
						SeparateGenerationCredit: true,
						Description:              fmt.Sprintf("SDG&E NBT Holiday Export Credit (Hour %d)", hour),
					})
				}
			}
		}
	}
	periods = append(periods, types.UtilityFeesPeriod{TimePeriod: types.TimePeriod{LocationPtr: ptLocation}, DollarsPerKWH: nbc, GridAdditional: true, Description: "SDG&E Non-Bypassable Charges"})
	return periods
}

func getSDGEPCIA(t time.Time) float64 {
	august12026 := time.Date(2026, time.August, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(august12026) {
		return 0.04771 // 8/1/2026 PCIA
	}
	return 0.04987 // 4/1/2026 PCIA
}

func getUDCRate(plan, season, period string, t time.Time) float64 {
	august12026 := time.Date(2026, time.August, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(august12026) {
		switch plan {
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
				return 0.32601
			}
		case "sdge_tou_dr2":
			switch period {
			case "On-Peak":
				if season == "summer" {
					return 0.33063
				}
				return 0.32601
			case "Off-Peak":
				if season == "summer" {
					return 0.32397
				}
				return 0.32601
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
	switch plan {
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
			return 0.34061
		}
	case "sdge_tou_dr2":
		switch period {
		case "On-Peak":
			if season == "summer" {
				return 0.34510
			}
			return 0.34061
		case "Off-Peak":
			if season == "summer" {
				return 0.33863
			}
			return 0.34061
		}
	case "sdge_tou_elec":
		switch period {
		case "On-Peak", "Off-Peak", "Super Off-Peak":
			return 0.26288
		}
	case "sdge_dr_ses":
		switch period {
		case "On-Peak", "Off-Peak", "Super Off-Peak":
			return 0.25957
		}
	}
	return 0.0
}

func getSDGEBundledEECC(plan, season, period string, t time.Time) float64 {
	august12026 := time.Date(2026, time.August, 1, 0, 0, 0, 0, ptLocation)
	if !t.Before(august12026) {
		switch plan {
		case "sdge_ev_tou", "sdge_ev_tou_2", "sdge_ev_tou_5", "sdge_tou_elec", "sdge_dr_ses":
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
	switch plan {
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

// getSDCPRate returns the commodity generation rate for SDCP unbundled service.
func getSDCPRate(plan, location, tier, season, period string) float64 {
	// Map options to standard SDCP plan name
	var sdcpPlan string
	switch plan {
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
	case "sdge_tou_elec", "sdge_dr_ses":
		sdcpPlan = "TOU-ELEC"
	default:
		return 0.0
	}

	rates2021v := map[string]map[string]map[string]map[string]float64{
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

	premium := 0.0
	if tier == "sdcp_power_100" {
		premium = 0.01
	}

	if planMap, ok := rates[sdcpPlan]; ok {
		if tierMap, ok := planMap[tierKey]; ok {
			if seasonMap, ok := tierMap[season]; ok {
				if val, ok := seasonMap[period]; ok {
					return val + premium
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
	}

	return types.UtilityProviderInfo{
		ID:   "sdge",
		Name: "San Diego Gas & Electric (SDG&E)",
		Rates: []types.UtilityRateInfo{
			{
				ID:      "sdge_ev_tou",
				Name:    "Schedule EV-TOU (Separately Metered EV)",
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
