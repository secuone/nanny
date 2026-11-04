package i18n

// es strings of the pane menu; they are merged into the table in init.
var esPane = map[string]string{
	"Rename pane": "Renombrar panel",
	"Empty goes back to the program's own title.": "Vacío vuelve al título del propio programa.",
	"Rename pane…":              "Renombrar panel…",
	"Send right-clicks to pane": "Enviar clics derechos al panel",
	"right-clicks go to the program; right-click the pane's border for this menu": "los clics derechos van al programa; clic derecho en el borde del panel para este menú",
	"rename pane": "renombrar panel",
}

func init() { merge(es, esPane) }
