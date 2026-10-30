package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestPricesDecode(t *testing.T) {
	c := Default()
	if _, err := toml.Decode("[prices.opus]\ninput = 3.5\ncache_read = 0.1\n", &c); err != nil {
		t.Fatal(err)
	}
	if p := c.Prices["opus"]; p.Input != 3.5 || p.CacheRead != 0.1 || p.Output != 0 {
		t.Errorf("prices: %+v", c.Prices)
	}
}
