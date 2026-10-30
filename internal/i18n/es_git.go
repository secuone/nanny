package i18n

// es strings of the Git view; they are merged into the table in init.
var esGit = map[string]string{}

func init() { merge(es, esGit) }
