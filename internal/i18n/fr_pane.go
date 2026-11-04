package i18n

// fr strings of the pane menu; they are merged into the table in init.
var frPane = map[string]string{
	"Rename pane": "Renommer le panneau",
	"Empty goes back to the program's own title.": "Vide, on revient au titre du programme.",
	"Rename pane…":              "Renommer le panneau…",
	"Send right-clicks to pane": "Envoyer les clics droits au panneau",
	"right-clicks go to the program; right-click the pane's border for this menu": "les clics droits vont au programme ; clic droit sur la bordure du panneau pour ce menu",
	"rename pane": "renommer le panneau",
}

func init() { merge(fr, frPane) }
