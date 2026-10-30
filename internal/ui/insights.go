package ui

import tea "github.com/charmbracelet/bubbletea"

// insightsView is the Insights view (I): what your Claude usage costs and
// does, read from the local transcripts. Empty until it is written.
type insightsView struct{ featBase }

func (v *insightsView) view(m *Model, w, h int) string {
	return m.st.panel(T("Insights"), "", w, h, !m.focusR)
}
func (v *insightsView) detail(m *Model, w, h int) string   { return m.st.panel("", "", w, h, m.focusR) }
func (v *insightsView) key(m *Model, k tea.KeyMsg) tea.Cmd { return nil }
