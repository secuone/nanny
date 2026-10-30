package i18n

// fr strings of the Git view; they are merged into the table in init.
var frGit = map[string]string{}

func init() { merge(fr, frGit) }
