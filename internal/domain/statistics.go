package domain

// StatisticsCounts contains additive counters only. Distinct item counts belong
// to individual buckets and must never be added across buckets.
type StatisticsCounts struct {
	ItemReports     int64            `json:"item_reports"`
	NewItems        int64            `json:"new_items"`
	Exposures       int64            `json:"exposures"`
	Clicks          int64            `json:"clicks"`
	MissingEventIDs int64            `json:"missing_event_ids"`
	DuplicateEvents int64            `json:"duplicate_events"`
	Events          map[string]int64 `json:"events"`
}

type StatisticsHour struct {
	Hour          int              `json:"hour"`
	ReportedItems int64            `json:"reported_items"`
	Counts        StatisticsCounts `json:"counts"`
}

type StatisticsSnapshot struct {
	GeneratedAt   int64  `json:"generated_at"`
	HistorySince  string `json:"history_since"`
	ItemTotal     int64  `json:"item_total"`
	ItemAvailable int64  `json:"item_available"`
}

type StatisticsDay struct {
	SchemaVersion int                `json:"schema_version"`
	AppID         string             `json:"appid"`
	Date          string             `json:"date"`
	Timezone      string             `json:"timezone"`
	Revision      int64              `json:"revision"`
	GeneratedAt   int64              `json:"generated_at"`
	ReportedItems int64              `json:"reported_items"`
	Counts        StatisticsCounts   `json:"counts"`
	Hours         []StatisticsHour   `json:"hours"`
	Snapshot      StatisticsSnapshot `json:"snapshot"`
}
