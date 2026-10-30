package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestInsightsDefaultsAndTable(t *testing.T) {
	var c Config
	if c.Insights.HistoryDays() != 90 || c.Insights.ExpensiveLimit() != 20 || c.Insights.ExportDir != "" {
		t.Errorf("defaults: %+v", c.Insights)
	}
	if _, err := toml.Decode("[insights]\ndays = 30\nexpensive_usd = 5.5\nexport_dir = \"/x\"\n", &c); err != nil {
		t.Fatal(err)
	}
	if c.Insights.HistoryDays() != 30 || c.Insights.ExpensiveLimit() != 5.5 || c.Insights.ExportDir != "/x" {
		t.Errorf("table: %+v", c.Insights)
	}
	c.Insights.Days, c.Insights.ExpensiveUSD = -1, -2
	if c.Insights.HistoryDays() != 90 || c.Insights.ExpensiveLimit() != 20 {
		t.Error("nonsense falls back to the defaults")
	}
}
