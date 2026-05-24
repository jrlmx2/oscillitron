//go:build darwin

package vram

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// darwinPriority places the unified-memory probe after dedicated-GPU
// probes (NVIDIA/AMD). On a Mac with no NVIDIA/AMD GPU attached, this
// is the natural winner.
const darwinPriority = 300

func init() {
	Register(darwinPriority, func() Probe { return darwinUnifiedProbe{} })
}

// darwinUnifiedProbe reads total physical memory via sysctl and free
// pages via vm_stat. Apple Silicon's unified-memory architecture means
// the GPU draws from the same pool as the CPU; we treat free unified
// memory as available "VRAM" — appropriate for our use case because
// Hermes/the inference engine allocates from this same pool.
type darwinUnifiedProbe struct{}

func (darwinUnifiedProbe) Name() string { return string(SourceDarwin) }

func (p darwinUnifiedProbe) Probe(ctx context.Context) (Report, error) {
	total, err := sysctlUint64(ctx, "hw.memsize")
	if err != nil {
		return Report{}, fmt.Errorf("darwin: hw.memsize: %w", err)
	}
	free, err := readVMStatFree(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("darwin: vm_stat: %w", err)
	}
	return Report{
		Source:         SourceDarwin,
		AvailableBytes: free,
		TotalBytes:     total,
		GPUs: []GPUReading{
			{Index: 0, Name: "unified", AvailableBytes: free, TotalBytes: total},
		},
	}, nil
}

func sysctlUint64(ctx context.Context, key string) (uint64, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "sysctl", "-n", key)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(out.String()), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", key, err)
	}
	return v, nil
}

// readVMStatFree reads vm_stat and returns the byte count of
// "free + inactive + speculative + purgeable" pages — the conservative
// approximation of memory the system would let a new allocation use.
// Page size is read from the first line of vm_stat ("page size of N
// bytes").
func readVMStatFree(ctx context.Context) (uint64, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "vm_stat")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	lines := strings.Split(out.String(), "\n")
	if len(lines) == 0 {
		return 0, fmt.Errorf("vm_stat empty")
	}
	pageSize := uint64(4096)
	for _, prefix := range []string{"Mach Virtual Memory Statistics: (page size of ", "page size of "} {
		if i := strings.Index(lines[0], prefix); i >= 0 {
			rest := lines[0][i+len(prefix):]
			if j := strings.Index(rest, " bytes"); j >= 0 {
				if v, err := strconv.ParseUint(rest[:j], 10, 64); err == nil {
					pageSize = v
					break
				}
			}
		}
	}
	var totalPages uint64
	for _, line := range lines[1:] {
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		label := strings.TrimSpace(strings.ToLower(line[:colon]))
		value := strings.TrimSpace(line[colon+1:])
		value = strings.TrimRight(value, ".")
		switch label {
		case "pages free", "pages inactive", "pages speculative", "pages purgeable":
			if v, err := strconv.ParseUint(value, 10, 64); err == nil {
				totalPages += v
			}
		}
	}
	return totalPages * pageSize, nil
}
