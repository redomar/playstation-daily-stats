package main

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type diskStat struct {
	TotalBytes uint64
	AvailBytes uint64
}

func statfsPath(path string) (diskStat, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return diskStat{}, err
	}

	blockSize := uint64(stat.Bsize)
	total := stat.Blocks * blockSize
	return diskStat{
		TotalBytes: total,
		AvailBytes: stat.Bavail * blockSize,
	}, nil
}

func dirBytes(root string) uint64 {
	var total uint64
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			total += uint64(info.Size())
		}
		return nil
	})
	return total
}

type memStat struct {
	LimitBytes      uint64
	WorkingSetBytes uint64

	HostTotalBytes     uint64
	HostAvailableBytes uint64
}

const cgroupV2Root = "/sys/fs/cgroup"

func readCgroupUint(path string) (uint64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value := strings.TrimSpace(string(data))
	if value == "max" {
		return 0, true
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func cgroupStatField(path, key string) (uint64, bool) {
	file, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		field, value, ok := strings.Cut(scanner.Text(), " ")
		if !ok || field != key {
			continue
		}
		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func containerMemory(memory *memStat) {
	if current, ok := readCgroupUint(cgroupV2Root + "/memory.current"); ok {
		memory.LimitBytes, _ = readCgroupUint(cgroupV2Root + "/memory.max")
		memory.WorkingSetBytes = current
		if inactive, ok := cgroupStatField(cgroupV2Root+"/memory.stat", "inactive_file"); ok && inactive < current {
			memory.WorkingSetBytes = current - inactive
		}
		return
	}

	const cgroupV1Root = "/sys/fs/cgroup/memory"
	if current, ok := readCgroupUint(cgroupV1Root + "/memory.usage_in_bytes"); ok {
		memory.WorkingSetBytes = current
		if inactive, ok := cgroupStatField(cgroupV1Root+"/memory.stat", "total_inactive_file"); ok && inactive < current {
			memory.WorkingSetBytes = current - inactive
		}
		if limit, ok := readCgroupUint(cgroupV1Root + "/memory.limit_in_bytes"); ok && limit < 1<<62 {
			memory.LimitBytes = limit
		}
	}
}

func hostMemory(memory *memStat) error {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, rest, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		kilobytes, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		bytes := kilobytes * 1024
		switch key {
		case "MemTotal":
			memory.HostTotalBytes = bytes
		case "MemAvailable":
			memory.HostAvailableBytes = bytes
		}
	}
	return scanner.Err()
}

type CapacityReport struct {
	ArchiveBytes       uint64
	StorageUsedPercent float64
	MemoryUsedPercent  float64
}

func percentage(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	if used >= total {
		return 100
	}
	return float64(used) / float64(total) * 100
}

func BuildCapacityReport(volumePath string) (CapacityReport, error) {
	var report CapacityReport

	volume, err := statfsPath(volumePath)
	if err != nil {
		return report, err
	}
	report.ArchiveBytes = dirBytes(volumePath)
	storageUsed := uint64(0)
	if volume.AvailBytes < volume.TotalBytes {
		storageUsed = volume.TotalBytes - volume.AvailBytes
	}
	report.StorageUsedPercent = percentage(storageUsed, volume.TotalBytes)

	var memory memStat
	containerMemory(&memory)
	_ = hostMemory(&memory)

	memoryTotal := memory.HostTotalBytes
	memoryAvailable := memory.HostAvailableBytes
	if memory.LimitBytes > 0 && (memory.HostTotalBytes == 0 || memory.LimitBytes < memory.HostTotalBytes) {
		memoryTotal = memory.LimitBytes
		if memory.WorkingSetBytes < memory.LimitBytes {
			memoryAvailable = memory.LimitBytes - memory.WorkingSetBytes
		} else {
			memoryAvailable = 0
		}
	}

	memoryUsed := uint64(0)
	if memoryAvailable < memoryTotal {
		memoryUsed = memoryTotal - memoryAvailable
	}
	report.MemoryUsedPercent = percentage(memoryUsed, memoryTotal)
	return report, nil
}
