package config

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"testing"
)

var hexRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// Every theme defines every key, and keeps rows, dim text and state colors readable.
func TestThemesComplete(t *testing.T) {
	if _, ok := Themes[DefaultTheme]; !ok {
		t.Fatalf("default theme %q missing", DefaultTheme)
	}
	for name, p := range Themes {
		v := reflect.ValueOf(p)
		for i := 0; i < v.NumField(); i++ {
			if s := v.Field(i).String(); !hexRe.MatchString(s) {
				t.Errorf("%s.%s = %q", name, v.Type().Field(i).Name, s)
			}
		}
		need := func(what, fg, bg string, min float64) {
			if c := Contrast(fg, bg); c < min {
				t.Errorf("%s: %s contrast %.2f < %.1f", name, what, c, min)
			}
		}
		need("text/bg", p.Text, p.Bg, 6.5)
		need("text/selected row", p.Text, p.Surface, 5)
		need("subtle/bg", p.Subtle, p.Bg, 4.5)
		need("muted/bg", p.Muted, p.Bg, 3)
		need("muted/selected row", p.Muted, p.Surface, 2.3)
		need("selected row/bg", p.Surface, p.Bg, 1.15)
		need("border/bg", p.Overlay, p.Bg, 1.5)
		// text on a lit tab (peach), a hovered one (overlay) and a button (accent)
		for k, c := range map[string]string{"peach": p.Peach, "overlay": p.Overlay, "accent": p.Accent} {
			if best := max(Contrast(p.Bg, c), Contrast(p.Text, c)); best < 4 {
				t.Errorf("%s: text on %s contrast %.2f < 4", name, k, best)
			}
		}
		// state colors tell things apart: no two of them alike
		named := [][2]string{{"accent", p.Accent}, {"accent2", p.Accent2}, {"green", p.Green}, {"yellow", p.Yellow},
			{"red", p.Red}, {"blue", p.Blue}, {"peach", p.Peach}, {"teal", p.Teal}}
		for i, a := range named {
			for _, b := range named[i+1:] {
				if d := colorDist(a[1], b[1]); d < 15 {
					t.Errorf("%s: %s and %s look alike (%.0f)", name, a[0], b[0], d)
				}
			}
		}
		for k, c := range map[string]string{"accent": p.Accent, "accent2": p.Accent2, "green": p.Green, "yellow": p.Yellow,
			"red": p.Red, "blue": p.Blue, "peach": p.Peach, "teal": p.Teal} {
			need(k+"/bg", c, p.Bg, 3)
			need(k+"/selected row", c, p.Surface, 2.4)
		}
	}
}

func TestResolveOverrides(t *testing.T) {
	c := Default()
	c.Theme = "nope"
	c.Colors.Red = "#123456"
	p := c.Resolve()
	if p.Bg != Themes[DefaultTheme].Bg || p.Red != "#123456" {
		t.Errorf("resolve: %+v", p)
	}
}

// colorDist is how far apart two colors look (weighted RGB distance).
func colorDist(a, b string) float64 {
	var r1, g1, b1, r2, g2, b2 int
	fmt.Sscanf(a, "#%02x%02x%02x", &r1, &g1, &b1)
	fmt.Sscanf(b, "#%02x%02x%02x", &r2, &g2, &b2)
	dr, dg, db := float64(r1-r2), float64(g1-g2), float64(b1-b2)
	return math.Sqrt(dr*dr*0.3 + dg*dg*0.59 + db*db*0.11)
}
