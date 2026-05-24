package vram

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// rocmPriority places AMD's probe after NVIDIA but ahead of the
// platform-generic fallbacks.
const rocmPriority = 200

func init() {
	Register(rocmPriority, func() Probe { return rocmSMIProbe{} })
}

// rocmSMIProbe shells out to `rocm-smi --showmeminfo vram --csv`.
// Output columns vary across rocm-smi versions; we tolerate that by
// scanning the header row for the columns we want.
type rocmSMIProbe struct{}

func (rocmSMIProbe) Name() string { return string(SourceROCmSMI) }

func (p rocmSMIProbe) Probe(ctx context.Context) (Report, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "rocm-smi", "--showmeminfo", "vram", "--csv")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return Report{}, fmt.Errorf("rocm-smi: %w", err)
	}
	records, err := csv.NewReader(&out).ReadAll()
	if err != nil {
		return Report{}, fmt.Errorf("rocm-smi: parse csv: %w", err)
	}
	if len(records) < 2 {
		return Report{}, fmt.Errorf("rocm-smi: no GPU rows")
	}
	header := records[0]
	deviceCol := indexOfCol(header, "device", "name", "card series")
	totalCol := indexOfCol(header, "vram total memory (b)", "vram total")
	usedCol := indexOfCol(header, "vram total used memory (b)", "vram used")
	if totalCol < 0 || usedCol < 0 {
		return Report{}, fmt.Errorf("rocm-smi: missing memory columns in header: %v", header)
	}
	report := Report{Source: SourceROCmSMI}
	for i, row := range records[1:] {
		if len(row) <= maxInt(deviceCol, totalCol, usedCol) {
			continue
		}
		total, err := strconv.ParseUint(strings.TrimSpace(row[totalCol]), 10, 64)
		if err != nil {
			continue
		}
		used, err := strconv.ParseUint(strings.TrimSpace(row[usedCol]), 10, 64)
		if err != nil {
			continue
		}
		var name string
		if deviceCol >= 0 && deviceCol < len(row) {
			name = strings.TrimSpace(row[deviceCol])
		}
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
		return Report{}, fmt.Errorf("rocm-smi: parsed no GPUs")
	}
	return report, nil
}

// indexOfCol does case-insensitive header lookup with multiple
// candidate names (rocm-smi versions disagree on column names).
func indexOfCol(header []string, candidates ...string) int {
	for i, h := range header {
		hl := strings.ToLower(strings.TrimSpace(h))
		for _, c := range candidates {
			if hl == strings.ToLower(c) {
				return i
			}
		}
	}
	return -1
}

func maxInt(vs ...int) int {
	m := vs[0]
	for _, v := range vs[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
