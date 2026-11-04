package i18n

// de strings of the pane menu; they are merged into the table in init.
var dePane = map[string]string{
	"Rename pane": "Bereich umbenennen",
	"Empty goes back to the program's own title.": "Leer bringt den Titel des Programms zurück.",
	"Rename pane…":              "Bereich umbenennen…",
	"Send right-clicks to pane": "Rechtsklicks an den Bereich senden",
	"right-clicks go to the program; right-click the pane's border for this menu": "Rechtsklicks gehen an das Programm; Rechtsklick auf den Rand des Bereichs öffnet dieses Menü",
	"rename pane": "Bereich umbenennen",
}

func init() { merge(de, dePane) }
