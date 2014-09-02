package metrics

import (
	"inspeqtor/util"
	"io/ioutil"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var (
	timeRegexp = regexp.MustCompile("\\A(\\d+):(\\d\\d).(\\d\\d)\\z")
)

type processStorage struct {
	*storage
	path string
}

func NewProcessStore(path string, values ...interface{}) Store {
	store := &processStorage{
		&storage{map[string]*family{}},
		path,
	}

	store.declareGauge("memory", "rss", nil, displayInMB)
	store.declareGauge("memory", "vsz", nil, displayInMB)
	store.declareCounter("cpu", "user", tickPercentage, displayPercent)
	store.declareCounter("cpu", "system", tickPercentage, displayPercent)
	store.declareCounter("cpu", "total_user", tickPercentage, displayPercent)
	store.declareCounter("cpu", "total_system", tickPercentage, displayPercent)
	if len(values) > 0 {
		store.fill(values...)
	}
	return store
}

func (ps *processStorage) Collect(pid int) error {
	var err error

	ok, err := util.FileExists(ps.path)
	if err != nil {
		return err
	}

	if !ok {
		// we don't have the /proc filesystem, e.g. darwin or freebsd
		// use `ps` output instead.
		err = ps.capturePs(pid)
		if err != nil {
			return err
		}
	} else {
		err = ps.captureVm(pid)
		if err != nil {
			return err
		}

		err = ps.captureCpu(pid)
		if err != nil {
			return err
		}
	}

	return nil
}

/*
 * So many hacks in this.  OSX support can be seen as "bad" at best.
 */
func (ps *processStorage) capturePs(pid int) error {
	cmd := exec.Command("ps", "So", "rss,vsz,time,utime", "-p", strconv.Itoa(pid))
	sout, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	lines, err := util.ReadLines(sout)
	if err != nil {
		return err
	}

	fields := strings.Fields(lines[1])
	val, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return err
	}

	ps.save("memory", "rss", 1024*val)
	val, err = strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return err
	}
	ps.save("memory", "vsz", 1024*val)

	times := timeRegexp.FindStringSubmatch(fields[2])
	if times == nil {
		util.Debug("Unable to parse CPU time in " + lines[1])
		return nil
	}
	min, _ := strconv.ParseUint(times[1], 10, 32)
	sec, _ := strconv.ParseUint(times[2], 10, 32)
	cs, _ := strconv.ParseUint(times[3], 10, 32)

	ticks := min*60*100 + sec*100 + cs

	times = timeRegexp.FindStringSubmatch(fields[3])
	if times == nil {
		util.Debug("Unable to parse User time in " + lines[1])
		return nil
	}
	min, _ = strconv.ParseUint(times[1], 10, 32)
	sec, _ = strconv.ParseUint(times[2], 10, 32)
	cs, _ = strconv.ParseUint(times[3], 10, 32)

	uticks := min*60*100 + sec*100 + cs

	ps.save("cpu", "user", int64(uticks))
	ps.save("cpu", "system", int64(ticks-uticks))

	return nil
}

func (ps *processStorage) captureCpu(pid int) error {
	dir := ps.path + "/" + strconv.Itoa(int(pid))
	data, err := ioutil.ReadFile(dir + "/stat")
	if err != nil {
		return err
	}

	lines, err := util.ReadLines(data)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		utime, err := strconv.ParseInt(fields[13], 10, 64)
		if err != nil {
			return err
		}
		stime, err := strconv.ParseInt(fields[14], 10, 64)
		if err != nil {
			return err
		}
		cutime, err := strconv.ParseInt(fields[15], 10, 64)
		if err != nil {
			return err
		}
		cstime, err := strconv.ParseInt(fields[16], 10, 64)
		if err != nil {
			return err
		}
		ps.save("cpu", "user", utime)
		ps.save("cpu", "system", stime)
		ps.save("cpu", "total_user", cutime)
		ps.save("cpu", "total_system", cstime)
	}

	return nil
}

func (ps *processStorage) captureVm(pid int) error {
	dir := ps.path + "/" + strconv.Itoa(int(pid))
	data, err := ioutil.ReadFile(dir + "/status")
	if err != nil {
		return err
	}

	lines, err := util.ReadLines(data)
	if err != nil {
		return err
	}
	for _, line := range lines {
		if line[0] == 'V' {
			items := strings.Fields(line)
			switch items[0] {
			case "VmRSS:":
				val, err := strconv.ParseInt(items[1], 10, 64)
				if err != nil {
					return err
				}
				ps.save("memory", "rss", 1024*val)
			case "VmSize:":
				val, err := strconv.ParseInt(items[1], 10, 64)
				if err != nil {
					return err
				}
				ps.save("memory", "vsz", 1024*val)
			}
		}

	}

	return nil
}
