package i18n

// fr strings of the sidebar navigation; they are merged into the table in init.
var frNav = map[string]string{
	"%d more spaces":                         "%d espaces de plus",
	"%s runs outside nanny: reading it live": "%s tourne hors de nanny : lecture en direct",
	"no agents to go to":                     "aucun agent où aller",
	"No agents in %s":                        "Aucun agent dans %s",
	"see agents from all spaces":             "voir les agents de tous les espaces",
	"%d waiting elsewhere":                   "%d en attente ailleurs",
	"waiting elsewhere":                      "en attente ailleurs",
}

func init() { merge(fr, frNav) }
