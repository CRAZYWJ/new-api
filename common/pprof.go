package common

import (
	"fmt"
	"os"
	"runtime/pprof"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/cpu"
)

// Monitor 定时监控cpu使用率，超过阈值输出pprof文件
func Monitor() {
	for {
		percent, err := cpu.Percent(time.Second, false)
		if err != nil {
			panic(err)
		}
		if percent[0] > 90 {
			fmt.Println(LogText("cpu usage too high"))
			// write pprof file
			if _, err := os.Stat("./pprof"); os.IsNotExist(err) {
				err := os.Mkdir("./pprof", os.ModePerm)
				if err != nil {
					SysLog(LogText("failed to create the pprof directory: %s", err.Error()))
					continue
				}
			}
			f, err := os.Create("./pprof/" + fmt.Sprintf("cpu-%s.pprof", time.Now().Format("20060102150405")))
			if err != nil {
				SysLog(LogText("failed to create the pprof file: %s", err.Error()))
				continue
			}
			err = pprof.StartCPUProfile(f)
			if err != nil {
				SysLog(LogText("failed to start pprof: %s", err.Error()))
				continue
			}
			time.Sleep(10 * time.Second) // profile for 30 seconds
			pprof.StopCPUProfile()
			f.Close()
			prunePprofFiles(f.Name())
		}
		time.Sleep(30 * time.Second)
	}
}

// prunePprofFiles 删除最旧的cpu pprof文件，保证./pprof目录总量不超过50MB，exclude为刚生成不允许删除的文件
func prunePprofFiles(exclude string) {
	const maxTotalBytes = 50 * 1024 * 1024
	entries, err := os.ReadDir("./pprof")
	if err != nil {
		return
	}
	type profileFile struct {
		name    string
		size    int64
		modTime time.Time
	}
	var files []profileFile
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "cpu-") || !strings.HasSuffix(entry.Name(), ".pprof") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, profileFile{name: entry.Name(), size: info.Size(), modTime: info.ModTime()})
		total += info.Size()
	}
	if total <= maxTotalBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	for _, file := range files {
		if total <= maxTotalBytes {
			break
		}
		if file.name == exclude {
			continue
		}
		if err := os.Remove("./pprof/" + file.name); err == nil {
			total -= file.size
		}
	}
}
