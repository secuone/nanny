package ui

import tea "github.com/charmbracelet/bubbletea"

// jiraView is the Jira view (E): your issues. Empty until it is written.
type jiraView struct{ featBase }

func (j *jiraView) view(m *Model, w, h int) string     { return m.st.panel(T("Jira"), "", w, h, !m.focusR) }
func (j *jiraView) detail(m *Model, w, h int) string   { return m.st.panel("", "", w, h, m.focusR) }
func (j *jiraView) key(m *Model, k tea.KeyMsg) tea.Cmd { return nil }
