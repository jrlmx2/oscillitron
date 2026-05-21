// CLAUDE GENERATED
//go:build linux

package vram

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// linuxDRMPriority sits below NVIDIA / AMD-via-rocm-smi / Apple
// Silicon (which doesn't run on Linux anyway), catching Intel Arc and
// generic AMD where the vendor tools aren't installed.
const linuxDRMPriority = 400

// procMeminfoPriority is the ultimate fallback — when no GPU probe
// found a device, we use system memory as the budget. This is the
// honest answer for CPU-only deployments.
const procMeminfoPriority = 500

func init() {
	Register(linuxDRMPriority, func() Probe { return linuxDRMProbe{} })
	Register(procMeminfoPriority, func() Probe { return procMeminfoProbe{} })
}

// linuxDRMProbe walks /sys/class/drm/card*/device looking for
// mem_info_vram_total / mem_info_vram_used files. AMD's amdgpu driver
// and Intel's xe driver expose these; nouveau and others generally
// don't.
type linuxDRMProbe struct{}

func (linuxDRMProbe) Name() string { return string(SourceLinuxDRM) }

func (p linuxDRMProbe) Probe(ctx context.Context) (Report, error) {
	matches, err := filepath.Glob("/sys/class/drm/card*/device")
	if err != nil {
		return Report{}, fmt.Errorf("drm-sysfs: glob: %w", err)
	}
	report := Report{Source: SourceLinuxDRM}
	for i, dir := range matches {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		total, err := readUintFile(filepath.Join(dir, "mem_info_vram_total"))
		if err != nil {
			continue // device doesn't expose VRAM stats; skip
		}
		used, err := readUintFile(filepath.Join(dir, "mem_info_vram_used"))
		if err != nil {
			continue
		}
		name, _ := readStringFile(filepath.Join(dir, "vendor"))
		free := uint64(0)
		if total > used {
			free = total - used
		}
		report.GPUs = append(report.GPUs, GPUReading{
			Index:          i,
			Name:           name,
			AvailableBytes: free,
			TotalBytes:     total,
		})
		report.AvailableBytes += free
		report.TotalBytes += total
	}
	if len(report.GPUs) == 0 {
		return Report{}, fmt.Errorf("drm-sysfs: no GPU exposes mem_info_vram_*")
	}
	return report, nil
}

// procMeminfoProbe is the honest fallback — read system memory from
// /proc/meminfo when no GPU probe could speak up. Appropriate when
// the inference engine is running on CPU.
type procMeminfoProbe struct{}

func (procMeminfoProbe) Name() string { return string(SourceProcMeminfo) }

func (p procMeminfoProbe) Probe(ctx context.Context) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return Report{}, fmt.Errorf("proc-meminfo: open: %w", err)
	}
	defer f.Close()
	var totalKB, availKB uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		key := line[:colon]
		rest := strings.Fields(line[colon+1:])
		if len(rest) == 0 {
			continue
		}
		v, err := strconv.ParseUint(rest[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			totalKB = v
		case "MemAvailable":
			availKB = v
		}
	}
	if availKB == 0 {
		return Report{}, fmt.Errorf("proc-meminfo: MemAvailable not found")
	}
	return Report{
		Source:         SourceProcMeminfo,
		AvailableBytes: availKB * 1024,
		TotalBytes:     totalKB * 1024,
	}, nil
}

func readUintFile(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

func readStringFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
