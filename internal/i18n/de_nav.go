package i18n

// de strings of the sidebar navigation; they are merged into the table in init.
var deNav = map[string]string{
	"%d more spaces":                         "%d weitere Spaces",
	"%s runs outside nanny: reading it live": "%s läuft außerhalb von nanny: live mitlesen",
	"no agents to go to":                     "kein Agent zum Hinspringen",
	"No agents in %s":                        "Keine Agenten in %s",
	"see agents from all spaces":             "Agenten aller Spaces anzeigen",
	"%d waiting elsewhere":                   "%d wartet woanders",
	"waiting elsewhere":                      "wartet woanders",
}

func init() { merge(de, deNav) }
