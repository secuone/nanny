package i18n

// de strings of the quit key, palette and help; they are merged into the table in init.
var deChrome = map[string]string{
	"quit":                           "beenden",
	"Quit nanny: panes keep running": "nanny beenden: Panes laufen weiter",
	"Go to space %s":                 "Zum Space %s",
	"your PRs, the reviews waiting for you, branches and worktrees (GitHub through gh)": "deine PRs, wartende Reviews, Branches und Worktrees (GitHub über gh)",
	"this space's Git: branch, PR, worktrees":                                           "Git dieses Space: Branch, PR, Worktrees",
	"previous / next agent":                 "vorheriger / nächster Agent",
	"quit (asks first; panes keep running)": "beenden (fragt vorher; Panes laufen weiter)",
	"Quit key":                              "Taste zum Beenden",
	"Asks to quit nanny from anywhere, panes included. Turn it off if a program in your panes needs it (emacs, micro).": "Fragt nach dem Beenden von nanny, von überall, auch in den Panes. Ausschalten, wenn ein Programm in deinen Panes sie braucht (emacs, micro).",
}

func init() { merge(de, deChrome) }
