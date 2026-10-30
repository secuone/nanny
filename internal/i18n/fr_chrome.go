package i18n

// fr strings of the quit key, palette and help; they are merged into the table in init.
var frChrome = map[string]string{
	"quit":                           "quitter",
	"Quit nanny: panes keep running": "Quitter nanny : les panneaux continuent",
	"Go to space %s":                 "Aller à l'espace %s",
	"your PRs, the reviews waiting for you, branches and worktrees (GitHub through gh)": "tes PR, les revues qui t'attendent, branches et worktrees (GitHub via gh)",
	"this space's Git: branch, PR, worktrees":                                           "le Git de cet espace : branche, PR, worktrees",
	"previous / next agent":                 "agent précédent / suivant",
	"quit (asks first; panes keep running)": "quitter (demande avant ; les panneaux continuent)",
	"Quit key":                              "Touche pour quitter",
	"Asks to quit nanny from anywhere, panes included. Turn it off if a program in your panes needs it (emacs, micro).": "Demande à quitter nanny de partout, panneaux compris. Désactive-la si un programme de tes panneaux en a besoin (emacs, micro).",
}

func init() { merge(fr, frChrome) }
