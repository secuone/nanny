package i18n

// de strings of the sidebar navigation; they are merged into the table in init.
var deNav = map[string]string{
	"%d more spaces":                         "%d weitere Spaces",
	"%s runs outside nanny: reading it live": "%s läuft außerhalb von nanny: live mitlesen",
	"no agents to go to":                     "kein Agent zum Hinspringen",
}

func init() { merge(de, deNav) }
