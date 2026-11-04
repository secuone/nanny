package i18n

// es strings of the sidebar navigation; they are merged into the table in init.
var esNav = map[string]string{
	"%d more spaces":                                    "%d espacios más",
	"%s runs outside nanny: reading it live":            "%s corre fuera de nanny: lo lees en vivo",
	"no agents to go to":                                "no hay agentes a los que ir",
	"no agents in this space · all spaces has the rest": "no hay agentes en este espacio · el resto está en todos los espacios",
	"No agents in %s":                                   "No hay agentes en %s",
	"see agents from all spaces":                        "ver los agentes de todos los espacios",
	"%d waiting elsewhere":                              "%d esperando en otro sitio",
	"waiting elsewhere":                                 "esperando en otro sitio",
}

func init() { merge(es, esNav) }
