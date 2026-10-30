package ui

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/sources"
)

// The Monitor's containers and Tilt resources have their own actions: logs
// in a read-only pane, a shell inside, start, stop, restart, trigger…

// containerShell opens bash inside a container, else sh.
func containerShell(name string) string {
	return "docker exec -it " + shellQuote(name) + ` sh -c 'command -v bash >/dev/null && exec bash || exec sh'`
}

// watch opens a command in a read-only tab: a log you look at, never type
// into. shell, when set, is what s opens next to it.
func (m *Model) watch(label, command, shell string) tea.Cmd {
	cwd, _ := os.UserHomeDir()
	if m.cfg.Launch == config.LaunchInline {
		return m.run(cwd, label, command)
	}
	r, err := m.spawnTab(cwd, label, commandArgv(command))
	if err != nil {
		m.setStatus(T("could not start: ")+err.Error(), true)
		return nil
	}
	r.pane.ReadOnly = true
	r.pane.Shell = commandArgv(shell)
	m.focusRef(r)
	m.saveSpaces()
	return nil
}

// tool runs a short docker or tilt command in the background, then says how
// it went and looks again.
func (m *Model) tool(done string, argv ...string) tea.Cmd {
	m.setStatus(strings.Join(argv, " ")+"…", false)
	run := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return statusMsg{trunc(msg, 120), true}
		}
		return statusMsg{done, false}
	}
	return tea.Sequence(run, m.sourcesCmd())
}

// itemActions are the actions of a container or a Tilt resource, as menu items.
func (m *Model) itemActions(it sources.Item) []menuItem {
	var items []menuItem
	add := func(k, label string, run func(m *Model) tea.Cmd) {
		items = append(items, menuItem{label, k, run})
	}
	name := it.Ref
	switch it.Kind {
	case "container":
		add("L", T("Logs (read-only)"), func(m *Model) tea.Cmd {
			return m.watch(T("log")+" "+trunc(name, 20), "docker logs -f --tail 300 "+shellQuote(name), containerShell(name))
		})
		if !it.Stopped {
			add("s", T("Shell inside"), func(m *Model) tea.Cmd {
				return m.run("", trunc(name, 24), containerShell(name))
			})
			add("t", T("Live stats"), func(m *Model) tea.Cmd {
				return m.watch(T("stats")+" "+trunc(name, 18), "docker stats "+shellQuote(name), "")
			})
		}
		if it.URL != "" {
			add("⏎", T("Open in the browser"), func(m *Model) tea.Cmd { return m.openURL(it.URL) })
		}
		items = append(items, sep)
		if it.Stopped {
			add("S", T("Start"), func(m *Model) tea.Cmd { return m.tool(Tf("%s started", name), "docker", "start", name) })
		} else {
			add("r", T("Restart"), func(m *Model) tea.Cmd { return m.tool(Tf("%s restarted", name), "docker", "restart", name) })
			add("x", T("Stop"), func(m *Model) tea.Cmd { return m.tool(Tf("%s stopped", name), "docker", "stop", name) })
		}
		add("y", T("Copy name"), func(m *Model) tea.Cmd { return m.copy(name, T("name")) })
		if it.Stopped {
			add("D", T("Remove…"), func(m *Model) tea.Cmd {
				m.ask(Tf("Remove container %s?", name), T("It is stopped; its data outside volumes is lost."),
					func(m *Model, _ string) tea.Cmd { return m.tool(Tf("%s removed", name), "docker", "rm", name) })
				return nil
			})
		}
	case "tilt":
		add("L", T("Logs (read-only)"), func(m *Model) tea.Cmd {
			return m.watch("tilt "+trunc(name, 20), "tilt logs -f "+shellQuote(name), "")
		})
		if it.URL != "" {
			add("⏎", T("Open in the browser"), func(m *Model) tea.Cmd { return m.openURL(it.URL) })
		}
		add("u", T("Open in the Tilt UI"), func(m *Model) tea.Cmd {
			return m.openURL("http://localhost:10350/r/" + name + "/overview")
		})
		items = append(items, sep)
		if it.Stopped {
			add("S", T("Enable"), func(m *Model) tea.Cmd { return m.tool(Tf("%s enabled", name), "tilt", "enable", name) })
		} else {
			add("r", T("Rebuild (trigger)"), func(m *Model) tea.Cmd { return m.tool(Tf("%s triggered", name), "tilt", "trigger", name) })
			add("x", T("Disable"), func(m *Model) tea.Cmd { return m.tool(Tf("%s disabled", name), "tilt", "disable", name) })
		}
		add("y", T("Copy name"), func(m *Model) tea.Cmd { return m.copy(name, T("name")) })
	}
	return items
}

// itemAction runs the action of the selected container or Tilt resource
// bound to key k; false when it has none.
func (m *Model) itemAction(k string) (tea.Cmd, bool) {
	it := m.radar.sel()
	if it == nil || it.Kind == "" {
		return nil, false
	}
	for _, a := range m.itemActions(*it) {
		if a.key == k && a.run != nil {
			return a.run(m), true
		}
	}
	return nil, false
}

// itemMenu opens the actions of the selected container or Tilt resource.
func (m *Model) itemMenu(x, y int) bool {
	it := m.radar.sel()
	if it == nil || it.Kind == "" {
		return false
	}
	m.openMenu(it.Name, x, y, m.itemActions(*it))
	return true
}
