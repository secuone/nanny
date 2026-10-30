package i18n

// es strings of the quit key, palette and help; they are merged into the table in init.
var esChrome = map[string]string{
	"quit":                           "salir",
	"Quit nanny: panes keep running": "Salir de nanny: los paneles siguen",
	"Go to space %s":                 "Ir al espacio %s",
	"your PRs, the reviews waiting for you, branches and worktrees (GitHub through gh)": "tus PRs, las revisiones que te esperan, ramas y worktrees (GitHub vía gh)",
	"this space's Git: branch, PR, worktrees":                                           "el Git de este espacio: rama, PR, worktrees",
	"previous / next agent":                 "agente anterior / siguiente",
	"quit (asks first; panes keep running)": "salir (pregunta antes; los paneles siguen)",
	"Quit key":                              "Tecla para salir",
	"Asks to quit nanny from anywhere, panes included. Turn it off if a program in your panes needs it (emacs, micro).": "Pide salir de nanny desde cualquier sitio, también dentro de los paneles. Desactívala si un programa de tus paneles la necesita (emacs, micro).",
}

func init() { merge(es, esChrome) }
