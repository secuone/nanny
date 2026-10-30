package ui

import tea "github.com/charmbracelet/bubbletea"

// setupWizard is the first-run guide that connects Jira and GitHub, shown over
// the screen like the welcome. Nothing opens it until it is written.
type setupWizard struct{}

// maybeSetup opens the wizard when it was never done; the welcome calls it as it closes.
func (m *Model) maybeSetup() {}

func (m Model) setupKey(k tea.KeyMsg) (tea.Model, tea.Cmd) { return m, nil }

func (m Model) viewSetup() string { return "" }

// setupUpdate takes the messages of the wizard's own requests.
func (m *Model) setupUpdate(msg tea.Msg) (tea.Cmd, bool) { return nil, false }
