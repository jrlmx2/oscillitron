// CLAUDE GENERATED
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

// nvidiaPriority places the NVIDIA probe ahead of every other
// platform-specific source — when nvidia-smi reports, it's the most
// explicit signal we can get.
const nvidiaPriority = 100

func init() {
	Register(nvidiaPriority, func() Probe { return nvidiaSMIProbe{} })
}

// nvidiaSMIProbe shells out to `nvidia-smi` and parses memory.free /
// memory.total per GPU. Works on any OS that has nvidia-smi on PATH.
type nvidiaSMIProbe struct{}

func (nvidiaSMIProbe) Name() string { return string(SourceNvidiaSMI) }

func (p nvidiaSMIProbe) Probe(ctx context.Context) (Report, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "nvidia-smi",
		"--query-gpu=index,name,memory.free,memory.total",
		"--format=csv,noheader,nounits")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return Report{}, fmt.Errorf("nvidia-smi: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return Report{}, fmt.Errorf("nvidia-smi: no output")
	}
	report := Report{Source: SourceNvidiaSMI}
	for _, line := range lines {
		fields := splitCSV(line)
		if len(fields) < 4 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		free, err := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64)
		if err != nil {
			continue
		}
		total, err := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 64)
		if err != nil {
			continue
		}
		// nvidia-smi reports MiB by default — convert to bytes.
		const MiB = 1024 * 1024
		freeB := free * MiB
		totalB := total * MiB
		report.GPUs = append(report.GPUs, GPUReading{
			Index:          idx,
			Name:           strings.TrimSpace(fields[1]),
			AvailableBytes: freeB,
			TotalBytes:     totalB,
		})
		report.AvailableBytes += freeB
		report.TotalBytes += totalB
	}
	if len(report.GPUs) == 0 {
		return Report{}, fmt.Errorf("nvidia-smi: parsed no GPUs")
	}
	return report, nil
}

// splitCSV is a tiny CSV splitter for nvidia-smi's well-behaved
// comma-separated output (no quoted fields, no embedded commas).
// stdlib encoding/csv would be heavier than needed.
func splitCSV(line string) []string {
	out := strings.Split(line, ",")
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	return out
}
