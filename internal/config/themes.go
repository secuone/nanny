package config

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// DefaultTheme is used for new configs and unknown theme names.
const DefaultTheme = "nanny"

var Themes = map[string]Palette{
	"nanny": {Bg: "#14151f", Surface: "#262940", Overlay: "#3b3f5c", Text: "#e6e8f2", Subtle: "#b4b9d0", Muted: "#7d83a2",
		Accent: "#a78bfa", Accent2: "#f472b6", Green: "#7ee0a1", Yellow: "#f5c76b", Red: "#ff6b81", Blue: "#6cb6ff", Peach: "#ff9e6b", Teal: "#5eead4"},
	"nanny-light": {Bg: "#f7f7fb", Surface: "#e2e3f0", Overlay: "#b9bcd2", Text: "#272a3a", Subtle: "#454a63", Muted: "#6b7090",
		Accent: "#7c3aed", Accent2: "#c0267a", Green: "#15803d", Yellow: "#a16207", Red: "#d11a3a", Blue: "#1d5fd1", Peach: "#c2410c", Teal: "#0f766e"},
	"catppuccin-mocha": {Bg: "#1e1e2e", Surface: "#313244", Overlay: "#45475a", Text: "#cdd6f4", Subtle: "#a6adc8", Muted: "#6c7086",
		Accent: "#cba6f7", Accent2: "#f5c2e7", Green: "#a6e3a1", Yellow: "#f9e2af", Red: "#f38ba8", Blue: "#89b4fa", Peach: "#fab387", Teal: "#94e2d5"},
	"catppuccin-latte": {Bg: "#eff1f5", Surface: "#d8dce5", Overlay: "#bcc0cc", Text: "#4c4f69", Subtle: "#5c5f77", Muted: "#7c7f93",
		Accent: "#8839ef", Accent2: "#c84ba6", Green: "#378c24", Yellow: "#a86a0c", Red: "#d20f39", Blue: "#1e66f5", Peach: "#b84a06", Teal: "#179299"},
	"tokyo-night": {Bg: "#1a1b26", Surface: "#292e42", Overlay: "#414868", Text: "#c0caf5", Subtle: "#a9b1d6", Muted: "#737aa2",
		Accent: "#bb9af7", Accent2: "#7dcfff", Green: "#9ece6a", Yellow: "#e0af68", Red: "#f7768e", Blue: "#7aa2f7", Peach: "#ff9e64", Teal: "#73daca"},
	"dracula": {Bg: "#282a36", Surface: "#3a3d4e", Overlay: "#44475a", Text: "#f8f8f2", Subtle: "#d6d6e0", Muted: "#7b88b8",
		Accent: "#bd93f9", Accent2: "#ff79c6", Green: "#50fa7b", Yellow: "#f1fa8c", Red: "#ff5555", Blue: "#82aaff", Peach: "#ffb86c", Teal: "#8be9fd"},
	"nord": {Bg: "#2e3440", Surface: "#3b4252", Overlay: "#4c566a", Text: "#eceff4", Subtle: "#d8dee9", Muted: "#7b88a1",
		Accent: "#5e9fd8", Accent2: "#b48ead", Green: "#a3be8c", Yellow: "#ebcb8b", Red: "#d06f79", Blue: "#81a1c1", Peach: "#d08770", Teal: "#8fbcbb"},
	"gruvbox": {Bg: "#282828", Surface: "#3c3836", Overlay: "#504945", Text: "#ebdbb2", Subtle: "#d5c4a1", Muted: "#928374",
		Accent: "#d3869b", Accent2: "#e7a0c0", Green: "#b8bb26", Yellow: "#fabd2f", Red: "#fb4934", Blue: "#83a598", Peach: "#fe8019", Teal: "#8ec07c"},
	"rose-pine": {Bg: "#191724", Surface: "#26233a", Overlay: "#403d52", Text: "#e0def4", Subtle: "#908caa", Muted: "#6e6a86",
		Accent: "#c4a7e7", Accent2: "#ebbcba", Green: "#a6d189", Yellow: "#f6c177", Red: "#eb6f92", Blue: "#5a9bb8", Peach: "#f08c5a", Teal: "#9ccfd8"},
	"kanagawa": {Bg: "#1f1f28", Surface: "#2a2a37", Overlay: "#54546d", Text: "#dcd7ba", Subtle: "#c8c093", Muted: "#727169",
		Accent: "#957fb8", Accent2: "#d27e99", Green: "#98bb6c", Yellow: "#e6c384", Red: "#e82424", Blue: "#7e9cd8", Peach: "#ffa066", Teal: "#7aa89f"},
	"one-dark": {Bg: "#282c34", Surface: "#353b45", Overlay: "#4b5263", Text: "#c5cbd6", Subtle: "#9da5b4", Muted: "#7f848e",
		Accent: "#c678dd", Accent2: "#e48bd3", Green: "#98c379", Yellow: "#e5c07b", Red: "#ef596f", Blue: "#61afef", Peach: "#d19a66", Teal: "#56b6c2"},
	"one-light": {Bg: "#fafafa", Surface: "#e5e5e6", Overlay: "#c2c2c7", Text: "#383a42", Subtle: "#4f525e", Muted: "#71737d",
		Accent: "#a626a4", Accent2: "#c2185b", Green: "#3f8a3e", Yellow: "#a06d00", Red: "#d13b2e", Blue: "#3366e0", Peach: "#c4510c", Teal: "#0f7ca6"},
	"solarized-dark": {Bg: "#002b36", Surface: "#073f4d", Overlay: "#2a5560", Text: "#bcc8c8", Subtle: "#93a1a1", Muted: "#6e868d",
		Accent: "#7f84d6", Accent2: "#e05593", Green: "#9aab1a", Yellow: "#c99a1a", Red: "#e8524f", Blue: "#3b9be0", Peach: "#e8763d", Teal: "#33b5ab"},
	"solarized-light": {Bg: "#fdf6e3", Surface: "#ebe2c8", Overlay: "#c9c2ac", Text: "#475b62", Subtle: "#586e75", Muted: "#7a8b8f",
		Accent: "#5b60b8", Accent2: "#c22c74", Green: "#677a00", Yellow: "#916d00", Red: "#cc2a27", Blue: "#1f73b0", Peach: "#b8410f", Teal: "#1f7d76"},
	"everforest": {Bg: "#2d353b", Surface: "#3d484d", Overlay: "#4f5b58", Text: "#d3c6aa", Subtle: "#b3ab95", Muted: "#859289",
		Accent: "#d699b6", Accent2: "#e3a8d0", Green: "#a7c080", Yellow: "#dbbc7f", Red: "#e67e80", Blue: "#7fa8d4", Peach: "#e69875", Teal: "#83c092"},
	"github-dark": {Bg: "#0d1117", Surface: "#1f2630", Overlay: "#30363d", Text: "#e6edf3", Subtle: "#c9d1d9", Muted: "#7d8590",
		Accent: "#bc8cff", Accent2: "#f778ba", Green: "#3fb950", Yellow: "#d29922", Red: "#f85149", Blue: "#58a6ff", Peach: "#ffa657", Teal: "#39c5cf"},
	"github-light": {Bg: "#ffffff", Surface: "#e7ecf0", Overlay: "#c4ccd4", Text: "#1f2328", Subtle: "#424a53", Muted: "#656d76",
		Accent: "#8250df", Accent2: "#bf3989", Green: "#1a7f37", Yellow: "#9a6700", Red: "#cf222e", Blue: "#0969da", Peach: "#bc4c00", Teal: "#1b7c83"},
	"monokai": {Bg: "#272822", Surface: "#3e3d32", Overlay: "#575648", Text: "#f8f8f2", Subtle: "#cfcfc2", Muted: "#8f8a75",
		Accent: "#ae81ff", Accent2: "#f92672", Green: "#a6e22e", Yellow: "#e6db74", Red: "#ff5f4a", Blue: "#66d9ef", Peach: "#fd971f", Teal: "#a1efe4"},
	"ayu": {Bg: "#0d1017", Surface: "#1c222c", Overlay: "#2d3640", Text: "#bfbdb6", Subtle: "#a5a39c", Muted: "#6c7380",
		Accent: "#e6b450", Accent2: "#d2a6ff", Green: "#aad94c", Yellow: "#ffe08a", Red: "#f07178", Blue: "#59c2ff", Peach: "#ff8f40", Teal: "#95e6cb"},
	"high-contrast": {Bg: "#000000", Surface: "#1f2a4d", Overlay: "#8a8a8a", Text: "#ffffff", Subtle: "#e6e6e6", Muted: "#b8b8b8",
		Accent: "#c792ff", Accent2: "#ff7ae0", Green: "#4dff74", Yellow: "#ffe14d", Red: "#ff5c5c", Blue: "#5cb3ff", Peach: "#ffab5c", Teal: "#33f0d6"},
}

func ThemeNames() []string {
	n := make([]string, 0, len(Themes))
	for k := range Themes {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

func joinThemes() string { return strings.Join(ThemeNames(), ", ") }

// Light reports whether the palette is meant for a light background.
func (p Palette) Light() bool { return luminance(p.Bg) > 0.4 }

// Contrast is the WCAG contrast ratio between two hex colors (1–21).
func Contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(hex string) float64 {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0
	}
	ch := func(c uint64) float64 {
		f := float64(c) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(v>>16&0xff) + 0.7152*ch(v>>8&0xff) + 0.0722*ch(v&0xff)
}

// Resolve returns the theme palette with the user's overrides applied.
func (c Config) Resolve() Palette {
	p, ok := Themes[c.Theme]
	if !ok {
		p = Themes[DefaultTheme]
	}
	o := c.Colors
	pick := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	pick(&p.Bg, o.Bg)
	pick(&p.Surface, o.Surface)
	pick(&p.Overlay, o.Overlay)
	pick(&p.Text, o.Text)
	pick(&p.Subtle, o.Subtle)
	pick(&p.Muted, o.Muted)
	pick(&p.Accent, o.Accent)
	pick(&p.Accent2, o.Accent2)
	pick(&p.Green, o.Green)
	pick(&p.Yellow, o.Yellow)
	pick(&p.Red, o.Red)
	pick(&p.Blue, o.Blue)
	pick(&p.Peach, o.Peach)
	pick(&p.Teal, o.Teal)
	return p
}
