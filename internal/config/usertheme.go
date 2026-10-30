package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// User themes live one per file in ~/.config/nanny/themes/<name>.toml, so
// they can be shared as is. A file names the theme it starts from (base) and
// the colors it changes; anything missing comes from the base.

// ThemeFile is what a theme file holds.
type ThemeFile struct {
	Base   string  `toml:"base,omitempty"`
	Colors Palette `toml:"colors"`
}

var (
	builtin   = map[string]bool{}
	nameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	hexColor  = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	ErrName   = errors.New("theme names are 1-32 of a-z 0-9 - _")
	ErrTaken  = errors.New("a theme with that name already exists")
	ErrBuilt  = errors.New("built-in themes can't be changed")
	ErrNoFile = errors.New("not a theme file")
)

func init() {
	for n := range Themes {
		builtin[n] = true
	}
}

// ThemesDir is where user themes are kept.
func ThemesDir() string { return filepath.Join(Dir(), "themes") }

func themePath(name string) string { return filepath.Join(ThemesDir(), name+".toml") }

// Custom reports whether name is a user theme (one that can be edited).
func Custom(name string) bool { _, ok := Themes[name]; return ok && !builtin[name] }

// ValidName says whether name can be a new user theme's name.
func ValidName(name string) error {
	switch {
	case !nameRe.MatchString(name):
		return ErrName
	case builtin[name]:
		return ErrTaken
	}
	return nil
}

// Fill completes p with base's colors and lower-cases them.
func Fill(p Palette, base string) Palette {
	b, ok := Themes[base]
	if !ok || base == "" {
		b = Themes[DefaultTheme]
	}
	pv, bv := reflect.ValueOf(&p).Elem(), reflect.ValueOf(b)
	for i := 0; i < pv.NumField(); i++ {
		if f := pv.Field(i); f.String() == "" {
			f.SetString(bv.Field(i).String())
		} else {
			f.SetString(strings.ToLower(f.String()))
		}
	}
	return p
}

// Validate checks every color is #rrggbb.
func Validate(p Palette) error {
	v := reflect.ValueOf(p)
	for i := 0; i < v.NumField(); i++ {
		if s := v.Field(i).String(); !hexColor.MatchString(s) {
			return fmt.Errorf("%s = %q: colors are #rrggbb", Tokens[i], s)
		}
	}
	return nil
}

// Tokens are the palette's keys, in field order.
var Tokens = func() []string {
	t := reflect.TypeOf(Palette{})
	out := make([]string, t.NumField())
	for i := range out {
		out[i] = strings.Split(t.Field(i).Tag.Get("toml"), ",")[0]
	}
	return out
}()

// Get and Set read and write a palette color by token name.
func (p Palette) Get(token string) string {
	for i, t := range Tokens {
		if t == token {
			return reflect.ValueOf(p).Field(i).String()
		}
	}
	return ""
}

func (p *Palette) Set(token, hex string) {
	for i, t := range Tokens {
		if t == token {
			reflect.ValueOf(p).Elem().Field(i).SetString(hex)
		}
	}
}

// LowContrast is a pair of colors too close to read one on the other.
type LowContrast struct {
	Fg, Bg string // tokens
	Ratio  float64
	Min    float64
}

// Contrasts lists what is hard to read in p: text and states on the
// background and on a selected row.
func Contrasts(p Palette) []LowContrast {
	var out []LowContrast
	need := func(fg, bg string, min float64) {
		if c := Contrast(p.Get(fg), p.Get(bg)); c < min {
			out = append(out, LowContrast{fg, bg, c, min})
		}
	}
	need("text", "bg", 4.5)
	need("text", "surface", 4.5)
	need("subtle", "bg", 3)
	need("muted", "bg", 2.5)
	for _, t := range []string{"accent", "accent2", "green", "yellow", "red", "blue", "peach", "teal"} {
		need(t, "bg", 2.5)
	}
	need("surface", "bg", 1.1)
	return out
}

// LoadThemes reads the user themes, replacing the ones read before. A file
// that can't be read is left out and reported.
func LoadThemes() []error {
	for n := range Themes {
		if !builtin[n] {
			delete(Themes, n)
		}
	}
	files, _ := filepath.Glob(filepath.Join(ThemesDir(), "*.toml"))
	sort.Strings(files)
	var errs []error
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".toml")
		p, err := readTheme(f)
		if err == nil {
			err = ValidName(name)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(f), err))
			continue
		}
		Themes[name] = p
	}
	return errs
}

func readTheme(path string) (Palette, error) {
	var tf ThemeFile
	md, err := toml.DecodeFile(path, &tf)
	if err != nil {
		return Palette{}, err
	}
	if !md.IsDefined("colors") && tf.Base == "" {
		return Palette{}, ErrNoFile
	}
	p := Fill(tf.Colors, tf.Base)
	return p, Validate(p)
}

// SaveTheme writes user theme name (new or not) and loads it.
func SaveTheme(name string, p Palette) error {
	if builtin[name] {
		return ErrBuilt
	}
	if err := ValidName(name); err != nil {
		return err
	}
	p = Fill(p, DefaultTheme)
	if err := Validate(p); err != nil {
		return err
	}
	if err := writeTheme(themePath(name), p); err != nil {
		return err
	}
	Themes[name] = p
	return nil
}

func writeTheme(path string, p Palette) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, _ = f.WriteString("# nanny theme — drop it in " + tildePath(ThemesDir()) + " or import it from the theme picker (T, i)\n")
	return toml.NewEncoder(f).Encode(ThemeFile{Colors: p})
}

// DeleteTheme removes a user theme.
func DeleteTheme(name string) error {
	if !Custom(name) {
		return ErrBuilt
	}
	if err := os.Remove(themePath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(Themes, name)
	return nil
}

// RenameTheme gives a user theme another name.
func RenameTheme(old, name string) error {
	if !Custom(old) {
		return ErrBuilt
	}
	if name == old {
		return nil
	}
	if err := ValidName(name); err != nil {
		return err
	}
	if _, ok := Themes[name]; ok {
		return ErrTaken
	}
	if err := SaveTheme(name, Themes[old]); err != nil {
		return err
	}
	return DeleteTheme(old)
}

// ExportTheme writes theme name (built-in or not) to path, a file or a folder.
func ExportTheme(name, path string) (string, error) {
	p, ok := Themes[name]
	if !ok {
		return "", fmt.Errorf("no theme %q", name)
	}
	path = expand(path)
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		path = filepath.Join(path, name+".toml")
	}
	return path, writeTheme(path, p)
}

// ImportTheme copies a theme file in, named after the file (or name+N when
// taken), and returns its name.
func ImportTheme(path string) (string, error) {
	path = expand(path)
	p, err := readTheme(path)
	if err != nil {
		return "", err
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	base = regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(base, "-")
	base = strings.Trim(base, "-_")
	if len(base) > 28 {
		base = base[:28]
	}
	if base == "" {
		base = "theme"
	}
	name := base
	for i := 2; ; i++ {
		if _, ok := Themes[name]; !ok {
			break
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name, SaveTheme(name, p)
}

// FreeName is base-copy, base-copy-2… whichever is not taken yet.
func FreeName(base string) string {
	base = strings.TrimSuffix(base, "-copy")
	if len(base) > 22 {
		base = base[:22]
	}
	name := base + "-copy"
	for i := 2; ; i++ {
		if _, ok := Themes[name]; !ok {
			return name
		}
		name = fmt.Sprintf("%s-copy-%d", base, i)
	}
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[2:])
	}
	return p
}

func tildePath(p string) string {
	h, _ := os.UserHomeDir()
	if h != "" && strings.HasPrefix(p, h+"/") {
		return "~" + p[len(h):]
	}
	return p
}
