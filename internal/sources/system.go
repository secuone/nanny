package sources

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/secuone/nanny/internal/i18n"
)

// ---------------------------------------------------------------- this machine

// Machine sums up the computer the agents run on: load, memory, disk and
// what Claude itself takes. Linux only; elsewhere it returns nothing.
func Machine() []Item {
	load, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	l := strings.Fields(string(load))
	if len(l) < 3 {
		return nil
	}
	l1, _ := strconv.ParseFloat(l[0], 64)
	cpus := runtime.NumCPU()
	mem := meminfo()
	total, avail := mem["MemTotal"], mem["MemAvailable"]
	usedPct := 0
	if total > 0 {
		usedPct = int(100 * (total - avail) / total)
	}
	var fs syscall.Statfs_t
	diskFree, diskPct := uint64(0), 0
	if syscall.Statfs(home, &fs) == nil && fs.Blocks > 0 {
		diskFree = fs.Bavail * uint64(fs.Bsize)
		diskPct = int(100 * fs.Bavail / fs.Blocks)
	}
	n, rss := claudeProcs()

	it := Item{Group: i18n.M("This machine"), Status: Idle, When: time.Now()}
	it.Name = i18n.Tf("load %.1f on %d cores · memory %d%% · disk %d%% free", l1, cpus, usedPct, diskPct)
	if l1 > float64(cpus) {
		it.Status = Running
	}
	if usedPct >= 90 || diskPct < 5 {
		it.Status = Failed
	}
	it.Detail = i18n.Tf("%d claude processes using %s", n, bytes(rss))
	up := ""
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			s, _ := strconv.ParseFloat(f[0], 64)
			up = dur(time.Duration(s) * time.Second)
		}
	}
	swap := ""
	if mem["SwapTotal"] > 0 {
		swap = fmt.Sprintf("%s / %s", bytes(mem["SwapTotal"]-mem["SwapFree"]), bytes(mem["SwapTotal"]))
	}
	it.Fields = []KV{
		{K: i18n.M("load"), V: fmt.Sprintf("%s · %s · %s (1 · 5 · 15 min)", l[0], l[1], l[2])},
		{K: i18n.M("cores"), V: strconv.Itoa(cpus)},
		{K: i18n.M("memory"), V: i18n.Tf("%s used of %s (%d%%)", bytes(total-avail), bytes(total), usedPct)},
		{K: i18n.M("swap"), V: swap},
		{K: i18n.M("disk"), V: i18n.Tf("%s free in %s (%d%%)", bytes(diskFree), home, diskPct)},
		{K: i18n.M("claude"), V: i18n.Tf("%d processes · %s", n, bytes(rss))},
		{K: i18n.M("up"), V: up},
	}
	return []Item{it}
}

// meminfo reads /proc/meminfo in bytes.
func meminfo() map[string]uint64 {
	out := map[string]uint64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fs := strings.Fields(v)
		if len(fs) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(fs[0], 10, 64)
		out[k] = n * 1024
	}
	return out
}

// claudeProcs counts the user's claude processes and their resident memory.
func claudeProcs() (n int, rss uint64) {
	for _, pid := range ownPids() {
		if comm(pid) != "claude" {
			continue
		}
		n++
		rss += procRSS(pid)
	}
	return n, rss
}

// ownPids lists the processes of the current user.
func ownPids() []int {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	uid := uint32(os.Getuid())
	var out []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if st, err := os.Stat("/proc/" + e.Name()); err == nil {
			if s, ok := st.Sys().(*syscall.Stat_t); ok && s.Uid == uid {
				out = append(out, pid)
			}
		}
	}
	return out
}

func comm(pid int) string {
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	return strings.TrimSpace(string(b))
}

func cmdline(pid int) string {
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	c := strings.Join(strings.Fields(strings.ReplaceAll(string(b), "\x00", " ")), " ") // one line
	if r := []rune(c); len(r) > 300 {
		c = string(r[:300]) + "…"
	}
	return c
}

func procRSS(pid int) uint64 {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0
	}
	pages, _ := strconv.ParseUint(f[1], 10, 64)
	return pages * uint64(os.Getpagesize())
}

// started is when a process began, from its start tick and the boot time.
func started(pid int) time.Time {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return time.Time{}
	}
	s := string(b)
	if i := strings.LastIndexByte(s, ')'); i >= 0 {
		s = s[i+1:]
	}
	f := strings.Fields(s)
	if len(f) < 20 {
		return time.Time{}
	}
	ticks, _ := strconv.ParseInt(f[19], 10, 64)
	st, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, l := range strings.Split(string(st), "\n") {
		if v, ok := strings.CutPrefix(l, "btime "); ok {
			bt, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return time.Unix(bt+ticks/100, 0)
		}
	}
	return time.Time{}
}

func bytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	}
	return fmt.Sprintf("%d KB", n>>10)
}

func dur(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return i18n.Tf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// ---------------------------------------------------------------- open ports

// Ports lists the TCP ports the user's own programs listen on: dev servers,
// databases, debuggers. ⏎ opens http://localhost:port.
// The sockets' owners are found walking every process's fds, the costly part
// of Ports; while the listening sockets stay the same, so do their owners.
var portCache struct {
	sync.Mutex
	key   string
	owner map[int]int
}

// portOwners maps each listening port to the process holding its socket.
func portOwners(inodes map[string]int) map[int]int {
	keys := make([]string, 0, len(inodes))
	for k := range inodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	key := strings.Join(keys, ",")
	portCache.Lock()
	defer portCache.Unlock()
	if key == portCache.key {
		alive := true
		for _, pid := range portCache.owner {
			if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err != nil {
				alive = false
			}
		}
		if alive {
			return portCache.owner
		}
	}
	owner := map[int]int{} // port → pid
	for _, pid := range ownPids() {
		fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			ln, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, fd.Name()))
			if err != nil || !strings.HasPrefix(ln, "socket:[") {
				continue
			}
			if port, ok := inodes[strings.TrimSuffix(strings.TrimPrefix(ln, "socket:["), "]")]; ok {
				if _, has := owner[port]; !has {
					owner[port] = pid
				}
			}
		}
	}
	portCache.key, portCache.owner = key, owner
	return owner
}

func Ports() []Item {
	inodes := map[string]int{} // socket inode → port
	local := map[int]bool{}    // port only reachable from this machine
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n")[1:] {
			fs := strings.Fields(l)
			if len(fs) < 10 || fs[3] != "0A" { // 0A = LISTEN
				continue
			}
			addr, hexPort, _ := strings.Cut(fs[1], ":")
			port, err := strconv.ParseInt(hexPort, 16, 32)
			if err != nil {
				continue
			}
			inodes[fs[9]] = int(port)
			lo := addr == "0100007F" || addr == "00000000000000000000000001000000"
			if prev, seen := local[int(port)]; seen {
				lo = prev && lo
			}
			local[int(port)] = lo
		}
	}
	if len(inodes) == 0 {
		return nil
	}
	owner := portOwners(inodes)
	ports := make([]int, 0, len(owner))
	for p := range owner {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	var items []Item
	for _, port := range ports {
		pid := owner[port]
		cwd, _ := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
		cmd := cmdline(pid)
		url := fmt.Sprintf("http://localhost:%d", port)
		reach := i18n.T("this machine only")
		if !local[port] {
			reach = i18n.T("the whole network")
		}
		where := cwd
		if strings.HasPrefix(where, home) {
			where = "~" + where[len(home):]
		}
		it := Item{Group: i18n.M("Open ports"), Name: fmt.Sprintf(":%d  %s", port, comm(pid)), Status: Running,
			Detail: where + " · " + cmd, When: started(pid), Cwd: cwd, URL: url}
		it.Fields = []KV{
			{K: i18n.M("address"), V: url},
			{K: i18n.M("reachable from"), V: reach},
			{K: i18n.M("process"), V: fmt.Sprintf("%s (pid %d)", comm(pid), pid)},
			{K: i18n.M("command"), V: cmd},
			{K: i18n.M("folder"), V: where},
			{K: i18n.M("memory"), V: bytes(procRSS(pid))},
		}
		items = append(items, it)
	}
	return items
}

// ---------------------------------------------------------------- containers

// Containers lists Docker containers, running ones first, when docker is
// installed and answers quickly.
func Containers() []Item {
	rows, ok := dockerRows()
	if !ok {
		return nil
	}
	var items, stopped []Item
	for _, c := range rows {
		st := Running
		switch {
		case strings.Contains(c.Status, "unhealthy"), strings.Contains(c.Status, "Restarting"):
			st = Failed
		case strings.Contains(c.Status, "Paused"):
			st = Idle
		case c.State != "" && c.State != "running":
			st = Off
		}
		it := Item{Group: i18n.M("Containers"), Name: c.Names, Status: st, Detail: c.Image + " · " + c.Status,
			Kind: "container", Ref: c.Names, Stopped: st == Off}
		it.When = c.Created
		if p := firstPort(c.Ports); p != "" {
			it.URL = "http://localhost:" + p
			it.Detail += " · :" + p
		}
		it.Fields = []KV{
			{K: i18n.M("image"), V: c.Image},
			{K: i18n.M("status"), V: c.Status},
			{K: i18n.M("ports"), V: c.Ports},
			{K: i18n.M("command"), V: strings.Trim(c.Command, `"`)},
			{K: i18n.M("id"), V: c.ID},
		}
		if it.Stopped {
			stopped = append(stopped, it)
			continue
		}
		items = append(items, it)
	}
	return append(items, stopped...)
}

type dockerRow struct {
	ID, Image, Names, Status, State, Ports, Command string
	Created                                         time.Time
}

// dockerRows are the containers, from the Docker socket, or `docker ps` when
// there is none to ask.
func dockerRows() ([]dockerRow, bool) {
	if b, ok := dockerAPI("/containers/json?all=1"); ok {
		var cs []struct {
			ID      string `json:"Id"`
			Names   []string
			Image   string
			Command string
			Created int64
			State   string
			Status  string
			Ports   []dockerPort
		}
		if json.Unmarshal(b, &cs) == nil {
			rows := make([]dockerRow, 0, len(cs))
			for _, c := range cs {
				var names []string
				for _, n := range c.Names {
					names = append(names, strings.TrimPrefix(n, "/"))
				}
				rows = append(rows, dockerRow{ID: c.ID[:min(12, len(c.ID))], Image: c.Image, Names: strings.Join(names, ","),
					Status: c.Status, State: c.State, Ports: dockerPorts(c.Ports), Command: c.Command, Created: time.Unix(c.Created, 0)})
			}
			return rows, true
		}
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}").Output()
	if err != nil {
		return nil, false
	}
	var rows []dockerRow
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var c struct{ ID, Image, Names, Status, State, Ports, Command, CreatedAt string }
		if json.Unmarshal([]byte(l), &c) != nil || c.ID == "" {
			continue
		}
		r := dockerRow{ID: c.ID, Image: c.Image, Names: c.Names, Status: c.Status, State: c.State, Ports: c.Ports, Command: c.Command}
		r.Created, _ = time.Parse("2006-01-02 15:04:05 -0700 MST", c.CreatedAt)
		rows = append(rows, r)
	}
	return rows, true
}

// ---------------------------------------------------------------- tilt

// Tilt lists the resources of a running Tilt (tilt get uiresources): what
// failed first, then what runs, then the ones turned off.
func Tilt() []Item {
	out, reached := tiltAPI()
	if !reached {
		if _, err := exec.LookPath("tilt"); err != nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var err error
		if out, err = exec.CommandContext(ctx, "tilt", "get", "uiresources", "-o", "json").Output(); err != nil {
			return nil
		}
	}
	if out == nil {
		return nil
	}
	var d struct {
		Items []struct {
			Metadata struct{ Name string }
			Status   struct {
				RuntimeStatus, UpdateStatus string
				DisableStatus               struct{ State string }
				EndpointLinks               []struct{ URL string }
				BuildHistory                []struct {
					Error      string
					FinishTime time.Time
				}
				Specs []struct{ Type string }
			}
		}
	}
	if json.Unmarshal(out, &d) != nil {
		return nil
	}
	sort.SliceStable(d.Items, func(i, j int) bool { return d.Items[i].Metadata.Name < d.Items[j].Metadata.Name }) // as `tilt get` lists them
	var failed, ok, off []Item
	for _, r := range d.Items {
		name, s := r.Metadata.Name, r.Status
		if name == "(Tiltfile)" {
			continue
		}
		it := Item{Group: i18n.M("Tilt"), Name: name, Kind: "tilt", Ref: name, Status: Running}
		kind := ""
		if len(s.Specs) > 0 {
			kind = s.Specs[0].Type
		}
		var errText string
		if len(s.BuildHistory) > 0 {
			errText = s.BuildHistory[0].Error
			it.When = s.BuildHistory[0].FinishTime
		}
		switch {
		case s.DisableStatus.State == "Disabled":
			it.Status, it.Stopped = Off, true
		case s.RuntimeStatus == "error" || s.UpdateStatus == "error":
			it.Status = Failed
		case s.UpdateStatus == "in_progress" || s.UpdateStatus == "pending" || s.RuntimeStatus == "pending":
			it.Status = Running // updating: busy, nothing for you to do
		case s.RuntimeStatus == "not_applicable" || s.RuntimeStatus == "none":
			it.Status = Done
		}
		it.Detail = kind + " · " + i18n.M("runtime") + " " + s.RuntimeStatus + " · " + i18n.M("update") + " " + s.UpdateStatus
		if len(s.EndpointLinks) > 0 {
			it.URL = s.EndpointLinks[0].URL
			it.Detail += " · " + it.URL
		}
		it.Fields = []KV{{K: i18n.M("runtime"), V: s.RuntimeStatus}, {K: i18n.M("update"), V: s.UpdateStatus}, {K: i18n.M("type"), V: kind}}
		for _, l := range s.EndpointLinks {
			it.Fields = append(it.Fields, KV{K: i18n.M("address"), V: l.URL})
		}
		if errText != "" {
			it.Fields = append(it.Fields, KV{K: i18n.M("error"), V: strings.TrimSpace(errText)})
		}
		switch it.Status {
		case Failed:
			failed = append(failed, it)
		case Off:
			off = append(off, it)
		default:
			ok = append(ok, it)
		}
	}
	return append(append(failed, ok...), off...)
}

// firstPort is the first host port of a docker ports column
// ("0.0.0.0:8080->80/tcp, …" → "8080").
func firstPort(ports string) string {
	for _, p := range strings.Split(ports, ",") {
		if host, _, ok := strings.Cut(strings.TrimSpace(p), "->"); ok {
			if i := strings.LastIndexByte(host, ':'); i >= 0 {
				return host[i+1:]
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------- cron, timers

var weekdays = []string{i18n.M("Sun"), i18n.M("Mon"), i18n.M("Tue"), i18n.M("Wed"), i18n.M("Thu"), i18n.M("Fri"), i18n.M("Sat")}

// Schedule says a cron schedule in words for the common shapes, else as is.
func Schedule(s string) string {
	switch s {
	case "@reboot":
		return i18n.T("at boot")
	case "@hourly":
		return i18n.T("every hour")
	case "@daily", "@midnight":
		return i18n.T("every day at 00:00")
	case "@weekly":
		return i18n.T("every week")
	case "@monthly":
		return i18n.T("every month")
	}
	f := strings.Fields(s)
	if len(f) != 5 {
		return s
	}
	min, hour, dom, mon, dow := f[0], f[1], f[2], f[3], f[4]
	if dom != "*" || mon != "*" {
		return s
	}
	days := ""
	switch dow {
	case "*":
	case "1-5":
		days = i18n.T("weekdays")
	default:
		var ds []string
		for _, d := range strings.Split(dow, ",") {
			if n, err := strconv.Atoi(d); err == nil && n >= 0 && n <= 7 {
				d = i18n.T(weekdays[n%7])
			}
			ds = append(ds, d)
		}
		days = strings.Join(ds, ", ")
	}
	at := func() string {
		if m, err := strconv.Atoi(min); err == nil {
			if h, err := strconv.Atoi(hour); err == nil {
				return fmt.Sprintf("%02d:%02d", h, m)
			}
		}
		return ""
	}
	var out string
	switch {
	case min == "*" && hour == "*":
		out = i18n.T("every minute")
	case strings.HasPrefix(min, "*/") && hour == "*":
		out = i18n.Tf("every %s min", min[2:])
	case min == "*":
		out = i18n.Tf("every minute, %s h", hour)
	case at() != "":
		out = i18n.Tf("at %s", at())
	case hour == "*":
		out = i18n.Tf("every hour at :%s", min)
	default:
		if _, err := strconv.Atoi(min); err == nil {
			out = i18n.Tf("at :%s, %s h", fmt.Sprintf("%02s", min), hour)
		} else {
			return s
		}
	}
	if days != "" {
		out += " · " + days
	}
	return out
}

// cronLog is the file a cron line appends its output to (">> file").
func cronLog(cmd string) string {
	f := strings.Fields(cmd)
	for i, w := range f {
		if (w == ">>" || w == ">") && i+1 < len(f) {
			return expand(strings.Replace(f[i+1], "~", home, 1))
		}
	}
	return ""
}

// Timers lists the user's systemd timers: what runs, when next and last.
func Timers() []Item {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "list-timers", "--all", "--output=json").Output()
	if err != nil {
		return nil
	}
	var ts []struct {
		Next      int64  `json:"next"`
		Last      int64  `json:"last"`
		Unit      string `json:"unit"`
		Activates string `json:"activates"`
	}
	if json.Unmarshal(out, &ts) != nil {
		return nil
	}
	var items []Item
	for _, t := range ts {
		it := Item{Group: i18n.M("Timers"), Name: strings.TrimSuffix(t.Unit, ".timer"), Status: Sched,
			Cmd: "journalctl --user -f -u " + t.Activates}
		next, last := "—", "—"
		if t.Next > 0 {
			nt := time.UnixMicro(t.Next)
			next = nt.Format("Mon 15:04")
			it.Detail = i18n.Tf("next %s", next)
		}
		if t.Last > 0 {
			it.When = time.UnixMicro(t.Last)
			last = it.When.Format("Mon 15:04")
		}
		it.Fields = []KV{{K: i18n.M("runs"), V: t.Activates}, {K: i18n.M("next"), V: next}, {K: i18n.M("last"), V: last}}
		items = append(items, it)
	}
	return items
}
