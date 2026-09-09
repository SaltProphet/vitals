package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/shirou/gopsutil/v4/sensors"
)

type coreOut struct {
	Index int     `json:"index"`
	Model string  `json:"model"`
	Speed float64 `json:"speed"`
	Pct   float64 `json:"pct"`
}

type memOut struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Free      uint64  `json:"free"`
	Cached    uint64  `json:"cached"`
	Buffers   uint64  `json:"buffers"`
	SwapTotal uint64  `json:"swapTotal"`
	SwapUsed  uint64  `json:"swapUsed"`
	UsedPct   float64 `json:"usedPct"`
	SwapPct   float64 `json:"swapPct"`
}

type diskOut struct {
	Mount     string  `json:"mount"`
	Fstype    string  `json:"fstype"`
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	UsedPct   float64 `json:"usedPct"`
}

type netOut struct {
	Name     string  `json:"name"`
	Internal bool    `json:"internal"`
	Rx       float64 `json:"rx"`
	Tx       float64 `json:"tx"`
	RxBytes  uint64  `json:"rxBytes"`
	TxBytes  uint64  `json:"txBytes"`
	Address  string  `json:"address"`
}

type procRow struct {
	Pid  int32   `json:"pid"`
	Ppid int32   `json:"ppid"`
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	RSS  uint64  `json:"rss"`
}

type procOut struct {
	Pid      int32   `json:"pid"`
	Name     string  `json:"name"`
	RSS      uint64  `json:"rss"`
	CPU      float64 `json:"cpu"`
	Uptime   float64 `json:"uptime"`
	Go       string  `json:"go"`
	Spinning bool    `json:"spinning"`
}

type snapshot struct {
	TS         int64     `json:"ts"`
	Hostname   string    `json:"hostname"`
	Platform   string    `json:"platform"`
	Release    string    `json:"release"`
	OS         string    `json:"os"`
	Arch       string    `json:"arch"`
	Uptime     uint64    `json:"uptime"`
	CPU        float64   `json:"cpu"`
	Cores      []coreOut `json:"cores"`
	Memory     memOut    `json:"memory"`
	Disks      []diskOut `json:"disks"`
	Network    []netOut  `json:"network"`
	Process    procOut    `json:"process"`
	Processes  []procRow  `json:"processes"`
	ThermalC   *float64   `json:"thermalC"`
	Loadavg    [3]float64 `json:"loadavg"`
	LoadPress  float64   `json:"loadPressure"`
}

type cpuTimes struct {
	user, system, idle, nice, iowait, irq, softirq, steal float64
}

var (
	mu       sync.Mutex
	prevCPU  []cpuTimes
	prevTot  cpuTimes
	prevTs   time.Time
	prevNet  map[string]net.IOCountersStat
	startAt  = time.Now()
	procMu   sync.Mutex
	prevProc = map[int32]procPrev{}
)

type procPrev struct {
	total float64
	ts    time.Time
}

func collect() snapshot {
	now := time.Now()
	info, _ := host.Info()
	hostname, _ := os.Hostname()
	if info != nil && info.Hostname != "" {
		hostname = info.Hostname
	}

	cpuInfos, _ := cpu.Info()
	times, _ := cpu.Times(true)
	model := "CPU"
	speed := 0.0
	if len(cpuInfos) > 0 {
		model = strings.TrimSpace(cpuInfos[0].ModelName)
		speed = cpuInfos[0].Mhz
	}

	mu.Lock()
	defer mu.Unlock()

	dt := now.Sub(prevTs).Seconds()
	if dt < 0.05 {
		dt = 0.05
	}

	cores := make([]coreOut, len(times))
	var sum cpuTimes
	for i, t := range times {
		ct := cpuTimes{
			user: t.User, system: t.System, idle: t.Idle, nice: t.Nice,
			iowait: t.Iowait, irq: t.Irq, softirq: t.Softirq, steal: t.Steal,
		}
		sum.user += ct.user
		sum.system += ct.system
		sum.idle += ct.idle
		sum.nice += ct.nice
		sum.iowait += ct.iowait
		sum.irq += ct.irq
		sum.softirq += ct.softirq
		sum.steal += ct.steal
		pct := 0.0
		if i < len(prevCPU) && !prevTs.IsZero() {
			pct = busyPct(prevCPU[i], ct)
		}
		mhz := speed
		if i < len(cpuInfos) && cpuInfos[i].Mhz > 0 {
			mhz = cpuInfos[i].Mhz
			if cpuInfos[i].ModelName != "" {
				model = strings.TrimSpace(cpuInfos[i].ModelName)
			}
		}
		cores[i] = coreOut{Index: i, Model: model, Speed: mhz, Pct: pct}
	}
	cpuPct := 0.0
	if !prevTs.IsZero() {
		cpuPct = busyPct(prevTot, sum)
	}
	prevCPU = make([]cpuTimes, len(times))
	for i, t := range times {
		prevCPU[i] = cpuTimes{
			user: t.User, system: t.System, idle: t.Idle, nice: t.Nice,
			iowait: t.Iowait, irq: t.Irq, softirq: t.Softirq, steal: t.Steal,
		}
	}
	prevTot = sum

	vm, _ := mem.VirtualMemory()
	sm, _ := mem.SwapMemory()
	memory := memOut{}
	if vm != nil {
		memory.Total = vm.Total
		memory.Used = vm.Used
		memory.Available = vm.Available
		memory.Free = vm.Free
		memory.Cached = vm.Cached
		memory.Buffers = vm.Buffers
		memory.UsedPct = vm.UsedPercent
	}
	if sm != nil {
		memory.SwapTotal = sm.Total
		memory.SwapUsed = sm.Used
		memory.SwapPct = sm.UsedPercent
	}

	disks := collectDisks()
	nets := collectNet(dt)
	prevTs = now

	var load [3]float64
	avg, err := cpu.Percent(0, false)
	if err == nil && len(avg) > 0 {
		// Windows has no load average; report busy-cores equivalent.
		n := float64(len(times))
		if n < 1 {
			n = 1
		}
		load[0] = (avg[0] / 100) * n
		load[1] = load[0]
		load[2] = load[0]
	}
	press := cpuPct
	if len(times) > 0 {
		press = (load[0] / float64(len(times))) * 100
		if press > 100 {
			press = 100
		}
	}

	proc := procOut{
		Pid:      int32(os.Getpid()),
		Name:     "Vitals",
		Go:       runtime.Version(),
		Uptime:   time.Since(startAt).Seconds(),
		Spinning: spinning.Load(),
	}
	if p, err := process.NewProcess(int32(os.Getpid())); err == nil {
		if m, err := p.MemoryInfo(); err == nil && m != nil {
			proc.RSS = m.RSS
		}
		if c, err := p.CPUPercent(); err == nil {
			proc.CPU = c
		}
		if n, err := p.Name(); err == nil && n != "" {
			proc.Name = n
		}
	}

	platform, release, osName := runtime.GOOS, "", runtime.GOOS
	if info != nil {
		platform = info.Platform
		release = info.PlatformVersion
		osName = info.OS
		if info.Uptime > 0 {
			// use below
		}
	}
	var uptime uint64
	if info != nil {
		uptime = info.Uptime
	}

	var thermal *float64
	if temps, err := sensors.SensorsTemperatures(); err == nil {
		for _, t := range temps {
			if t.Temperature > 0 {
				v := t.Temperature
				thermal = &v
				break
			}
		}
	}

	return snapshot{
		TS:        now.UnixMilli(),
		Hostname:  hostname,
		Platform:  platform,
		Release:   release,
		OS:        osName,
		Arch:      runtime.GOARCH,
		Uptime:    uptime,
		CPU:       cpuPct,
		Cores:     cores,
		Memory:    memory,
		Disks:     disks,
		Network:   nets,
		Process:   proc,
		Processes: collectProcs(),
		ThermalC:  thermal,
		Loadavg:   load,
		LoadPress: press,
	}
}

func collectProcs() []procRow {
	list, err := process.Processes()
	if err != nil {
		return nil
	}
	now := time.Now()
	procMu.Lock()
	defer procMu.Unlock()
	next := make(map[int32]procPrev, len(list))
	out := make([]procRow, 0, len(list))
	for _, p := range list {
		pid := p.Pid
		name, _ := p.Name()
		if name == "" {
			name = "pid " + strconv.Itoa(int(pid))
		}
		ppid, _ := p.Ppid()
		if ppid == pid {
			ppid = 0
		}
		var rss uint64
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			rss = mi.RSS
		}
		cpu := 0.0
		total := 0.0
		if t, err := p.Times(); err == nil && t != nil {
			total = t.User + t.System
			if prev, ok := prevProc[pid]; ok {
				dt := now.Sub(prev.ts).Seconds()
				if dt > 0.05 {
					cpu = (total - prev.total) / dt * 100
					if cpu < 0 {
						cpu = 0
					}
				}
			}
		}
		next[pid] = procPrev{total: total, ts: now}
		out = append(out, procRow{Pid: pid, Ppid: ppid, Name: name, CPU: cpu, RSS: rss})
	}
	prevProc = next
	return out
}

func busyPct(prev, next cpuTimes) float64 {
	prevIdle := prev.idle + prev.iowait
	nextIdle := next.idle + next.iowait
	prevNon := prev.user + prev.nice + prev.system + prev.irq + prev.softirq + prev.steal
	nextNon := next.user + next.nice + next.system + next.irq + next.softirq + next.steal
	prevTotal := prevIdle + prevNon
	nextTotal := nextIdle + nextNon
	td := nextTotal - prevTotal
	id := nextIdle - prevIdle
	if td <= 0 {
		return 0
	}
	pct := (1 - id/td) * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func collectDisks() []diskOut {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	out := make([]diskOut, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		fs := strings.ToLower(p.Fstype)
		if strings.Contains(fs, "squash") || strings.Contains(fs, "overlay") ||
			fs == "tmpfs" || fs == "devfs" || fs == "cdfs" || fs == "iso9660" ||
			fs == "udf" || fs == "proc" || fs == "sysfs" || fs == "cgroup2" {
			continue
		}
		u, err := disk.Usage(p.Mountpoint)
		if err != nil || u == nil || u.Total == 0 {
			continue
		}
		key := p.Device + "|" + p.Mountpoint
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, diskOut{
			Mount:     p.Mountpoint,
			Fstype:    p.Fstype,
			Total:     u.Total,
			Used:      u.Used,
			Available: u.Free,
			UsedPct:   u.UsedPercent,
		})
	}
	return out
}

func collectNet(dt float64) []netOut {
	counters, _ := net.IOCounters(true)
	ifaces, _ := net.Interfaces()
	addrByName := map[string]string{}
	internal := map[string]bool{}
	for _, iface := range ifaces {
		name := iface.Name
		internal[name] = hasFlag(iface.Flags, "loopback")
		for _, a := range iface.Addrs {
			if strings.Contains(a.Addr, ":") && !strings.Contains(a.Addr, ".") {
				continue
			}
			ip := a.Addr
			if i := strings.IndexByte(ip, '/'); i >= 0 {
				ip = ip[:i]
			}
			if ip != "" {
				addrByName[name] = ip
				break
			}
		}
		if _, ok := addrByName[name]; !ok && len(iface.Addrs) > 0 {
			ip := iface.Addrs[0].Addr
			if i := strings.IndexByte(ip, '/'); i >= 0 {
				ip = ip[:i]
			}
			addrByName[name] = ip
		}
	}

	out := make([]netOut, 0, len(counters))
	next := map[string]net.IOCountersStat{}
	for _, c := range counters {
		next[c.Name] = c
		n := netOut{
			Name:     c.Name,
			Internal: internal[c.Name] || c.Name == "lo" || c.Name == "Loopback Pseudo-Interface 1",
			RxBytes:  c.BytesRecv,
			TxBytes:  c.BytesSent,
			Address:  addrByName[c.Name],
		}
		if p, ok := prevNet[c.Name]; ok && dt > 0 {
			n.Rx = float64(diffU(c.BytesRecv, p.BytesRecv)) / dt
			n.Tx = float64(diffU(c.BytesSent, p.BytesSent)) / dt
		}
		out = append(out, n)
	}
	prevNet = next
	return out
}

func diffU(a, b uint64) uint64 {
	if a >= b {
		return a - b
	}
	return 0
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}
