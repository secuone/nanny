package i18n

// de strings of the Git view; they are merged into the table in init.
var deGit = map[string]string{}

func init() { merge(de, deGit) }
