// Package i18n translates the UI. English strings are the keys; each other
// language is a map from them (es.go, fr.go, de.go). Missing entries fall back
// to English, and a test checks every key used in code is in every table.
package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

// Langs are the supported languages, English first.
var Langs = []string{"en", "es", "fr", "de"}

var tables = map[string]map[string]string{"es": es, "fr": fr, "de": de}

var names = map[string]string{"en": "English", "es": "Español", "fr": "Français", "de": "Deutsch"}

var cur atomic.Value // string

func init() { cur.Store("en") }

// Set picks the language: a code from Langs, or "" / "auto" for the locale.
// Anything unsupported falls back to English.
func Set(lang string) { cur.Store(Resolve(lang)) }

// Lang is the language in use.
func Lang() string { return cur.Load().(string) }

// Resolve maps a config value to a supported code.
func Resolve(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" || strings.EqualFold(lang, "auto") {
		return Detect()
	}
	if c := norm(lang); c != "" {
		return c
	}
	return "en"
}

// Detect reads $LC_ALL, $LC_MESSAGES then $LANG, like libc: the first one set wins.
func Detect() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if c := norm(v); c != "" {
				return c
			}
			return "en" // C, POSIX or an unsupported locale
		}
	}
	return "en"
}

// norm turns "es_ES.UTF-8" or "fr-CA" into "es" / "fr"; "" if unsupported.
func norm(v string) string {
	v = strings.ToLower(v)
	if i := strings.IndexAny(v, "_-.@"); i >= 0 {
		v = v[:i]
	}
	if _, ok := names[v]; ok {
		return v
	}
	return ""
}

// Name is the language's own name ("Español").
func Name(lang string) string {
	if n, ok := names[lang]; ok {
		return n
	}
	return lang
}

// T translates s.
func T(s string) string {
	if t, ok := tables[Lang()][s]; ok {
		return t
	}
	return s
}

// Tf translates a format string and fills it.
func Tf(format string, a ...any) string { return fmt.Sprintf(T(format), a...) }

// Tn picks the singular or plural format for n and fills it, n first.
func Tn(n int, one, many string, a ...any) string {
	f := many
	if n == 1 || (n == 0 && Lang() == "fr") {
		f = one
	}
	return fmt.Sprintf(T(f), append([]any{n}, a...)...)
}

// M marks s as a key without translating it, for strings kept in tables and
// passed through T when shown.
func M(s string) string { return s }

// merge adds src to dst: a feature keeps its strings in its own file
// (es_git.go…) and merges them into the language's table.
func merge(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}
