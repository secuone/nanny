package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// keys scans the repo for T/Tf/Tn/M calls (bare in package ui, or i18n.X) and
// returns every literal key with where it was first seen.
func keys(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, root := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			if strings.Contains(p, "internal/i18n") {
				return nil
			}
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var name string
				switch fn := c.Fun.(type) {
				case *ast.Ident:
					if f.Name.Name == "ui" {
						name = fn.Name
					}
				case *ast.SelectorExpr:
					if x, ok := fn.X.(*ast.Ident); ok && x.Name == "i18n" {
						name = fn.Sel.Name
					}
				}
				var args []ast.Expr
				switch name {
				case "T", "Tf", "M":
					args = c.Args[:min(1, len(c.Args))]
				case "Tn":
					args = c.Args[1:min(3, len(c.Args))]
				default:
					return true
				}
				for _, a := range args {
					lit, ok := a.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						if name != "T" { // T(variable) is fine when the values are marked with M
							t.Errorf("%s: %s with a non-literal key", fset.Position(a.Pos()), name)
						}
						continue
					}
					s, _ := strconv.Unquote(lit.Value)
					if _, seen := out[s]; !seen {
						out[s] = fset.Position(lit.Pos()).String()
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

var verbRe = regexp.MustCompile(`%[-+# 0]*[0-9]*(\.[0-9]+)?[a-zA-Z%]`)

func verbs(s string) []string {
	v := verbRe.FindAllString(s, -1)
	sort.Strings(v)
	return v
}

// Every key used in code is in every table, with the same format verbs, and no table keeps stale keys.
func TestTablesComplete(t *testing.T) {
	ks := keys(t)
	if len(ks) < 100 {
		t.Fatalf("only %d keys found; is the scan broken?", len(ks))
	}
	for _, lang := range Langs[1:] {
		tb := tables[lang]
		var missing []string
		for k, at := range ks {
			v, ok := tb[k]
			if !ok {
				missing = append(missing, at+": "+strconv.Quote(k))
				continue
			}
			if !slices.Equal(verbs(k), verbs(v)) {
				t.Errorf("%s: verbs differ for %q → %q", lang, k, v)
			}
			if strings.TrimSpace(v) == "" && strings.TrimSpace(k) != "" {
				t.Errorf("%s: empty translation for %q", lang, k)
			}
		}
		sort.Strings(missing)
		for _, m := range missing {
			t.Errorf("%s: missing %s", lang, m)
		}
		for k := range tb {
			if _, ok := ks[k]; !ok {
				t.Errorf("%s: unused key %q", lang, k)
			}
		}
	}
}

func TestResolve(t *testing.T) {
	for _, c := range []struct{ all, msgs, lang, cfg, want string }{
		{"", "", "es_ES.UTF-8", "", "es"},
		{"", "", "es_ES.UTF-8", "auto", "es"},
		{"", "", "es_ES.UTF-8", "de", "de"},
		{"fr_CA.UTF-8", "", "es_ES.UTF-8", "", "fr"},
		{"", "de_AT", "es_ES", "", "de"},
		{"C.UTF-8", "", "es_ES.UTF-8", "", "en"},
		{"", "", "pt_BR.UTF-8", "", "en"},
		{"", "", "", "", "en"},
		{"", "", "", "klingon", "en"},
		{"", "", "", "FR", "fr"},
	} {
		t.Setenv("LC_ALL", c.all)
		t.Setenv("LC_MESSAGES", c.msgs)
		t.Setenv("LANG", c.lang)
		if got := Resolve(c.cfg); got != c.want {
			t.Errorf("%+v: got %s", c, got)
		}
	}
}

func TestTnAndFallback(t *testing.T) {
	defer Set("en")
	Set("en")
	if a, b := Tn(1, "%d agent", "%d agents"), Tn(2, "%d agent", "%d agents"); a != "1 agent" || b != "2 agents" {
		t.Errorf("en plural: %q %q", a, b)
	}
	Set("es")
	if got := T("no such key, ever"); got != "no such key, ever" {
		t.Errorf("fallback: %q", got)
	}
}

// A feature's own file adds its strings to a language's table.
func TestMerge(t *testing.T) {
	dst := map[string]string{"a": "1", "b": "2"}
	merge(dst, map[string]string{"b": "3", "c": "4"})
	if dst["a"] != "1" || dst["b"] != "3" || dst["c"] != "4" {
		t.Errorf("merge: %v", dst)
	}
}
