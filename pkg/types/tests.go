package types

import "time"

// ControllerHistoryDataset defines the JSON structure for history datasets.
type ControllerHistoryDataset struct {
	SiteID        string             `json:"siteId"`
	Period        string             `json:"period"`
	SimStart      time.Time          `json:"simStart"`
	SimEnd        time.Time          `json:"simEnd"`
	EnergyHistory []DailyEnergyStats `json:"energyHistory"`
	ActionHistory []Action           `json:"actionHistory"`
	PriceHistory  []Price            `json:"priceHistory"`
}
