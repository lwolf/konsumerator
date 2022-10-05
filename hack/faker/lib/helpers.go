package lib

import (
	"fmt"
	"k8s.io/klog/v2"
	"log"
	"math"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
)

const (
	cpuSharesCgroupV1 = "/sys/fs/cgroup/cpu/cpu.shares"
)

func cgroup2UnifiedMode() bool {
	return !FileExists(cpuSharesCgroupV1)
}

func GetCpuRequests() (uint64, error) {
	// to avoid failing when run on macos
	if runtime.GOOS == "darwin" {
		return 104, nil
	}
	if cgroup2UnifiedMode() {
		cpuRoot := "/sys/fs/cgroup"
		weight := readUInt64(cpuRoot, "cpu.weight")
		if weight > 0 {
			limit, err := convertCPUWeightToCPULimit(weight)
			if err != nil {
				log.Fatalf("GetSpec: Failed to read CPULimit from %q: %s", path.Join(cpuRoot, "cpu.weight"), err)
			} else {
				return limit, nil
			}
		}
		panic("cpu.requests is not set, fix the manifest")
	} else {
		limit := readUInt64("/sys/fs/cgroup/cpu", "cpu.shares")
		return limit, nil
	}
}

// logic to extract cgroups are borrowed from
// https://github.com/mhermida/cadvisor/blob/c3b109ff4a7a09f21db32fb1ec3316e9ae51cfe4/container/common/helpers.go
//
// Convert from [1-10000] to [2-262144]
func convertCPUWeightToCPULimit(weight uint64) (uint64, error) {
	const (
		// minWeight is the lowest value possible for cpu.weight
		minWeight = 1
		// maxWeight is the highest value possible for cpu.weight
		maxWeight = 10000
	)
	if weight < minWeight || weight > maxWeight {
		return 0, fmt.Errorf("convertCPUWeightToCPULimit: invalid cpu weight: %v", weight)
	}
	return 2 + ((weight-1)*262142)/9999, nil
}

func readUInt64(dirpath string, file string) uint64 {
	out := readString(dirpath, file)
	if out == "max" {
		return math.MaxUint64
	}
	if out == "" {
		return 0
	}

	val, err := strconv.ParseUint(out, 10, 64)
	if err != nil {
		klog.Errorf("readUInt64: Failed to parse int %q from file %q: %s", out, path.Join(dirpath, file), err)
		return 0
	}

	return val
}

func readString(dirpath string, file string) string {
	cgroupFile := path.Join(dirpath, file)

	// Read
	out, err := os.ReadFile(cgroupFile)
	if err != nil {
		// Ignore non-existent files
		if !os.IsNotExist(err) {
			klog.Warningf("readString: Failed to read %q: %s", cgroupFile, err)
		}
		return ""
	}
	return strings.TrimSpace(string(out))
}

func FileExists(file string) bool {
	if _, err := os.Stat(file); err != nil {
		return false
	}
	return true
}
