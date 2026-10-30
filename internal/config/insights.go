package config

// Insights is the [insights] table.
type Insights struct {
	Days         int     `toml:"days,omitempty"`          // how far back transcripts are read (default 90)
	ExpensiveUSD float64 `toml:"expensive_usd,omitempty"` // a session that costs more than this is listed as expensive (default 20)
	ExportDir    string  `toml:"export_dir,omitempty"`    // where x writes the week's JSON (default ~)
}

const (
	defaultInsightsDays = 90
	defaultExpensiveUSD = 20.0
)

// HistoryDays is how many days of transcripts Insights reads.
func (i Insights) HistoryDays() int {
	if i.Days < 1 {
		return defaultInsightsDays
	}
	return i.Days
}

// ExpensiveLimit is the cost in USD above which a session counts as expensive.
func (i Insights) ExpensiveLimit() float64 {
	if i.ExpensiveUSD <= 0 {
		return defaultExpensiveUSD
	}
	return i.ExpensiveUSD
}
