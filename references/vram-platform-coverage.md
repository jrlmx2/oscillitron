<!-- CLAUDE GENERATED -->

# VRAM probe — platform coverage & limitations

`pkg/vram` provides a stdlib-only, multi-platform read of available GPU memory used by the runner's dynamic concurrency cap. This doc captures the coverage matrix as **explicit limitations** — what works out of the box, where the seams are, and what to do when auto-detection can't tell us.

Updated 2026-05-21.

## Coverage matrix

| Platform / GPU | Probe | Mechanism | Confidence | Notes |
|---|---|---|---|---|
| NVIDIA — any OS | `nvidia-smi` | `os/exec` calls `nvidia-smi --query-gpu=memory.free,memory.total --format=csv,noheader,nounits` | High | Works wherever the NVIDIA driver + tooling is installed. Reports per-GPU. |
| AMD — Linux | `rocm-smi` + Linux DRM sysfs | `os/exec` (`rocm-smi --showmeminfo vram --csv`) with sysfs fallback (`/sys/class/drm/card*/device/mem_info_vram_total` and `mem_info_vram_used`) | Medium | rocm-smi requires the ROCm stack installed. sysfs fallback works on any recent AMD GPU with the amdgpu driver. |
| Apple Silicon (M-series) | `darwin-unified` | `sysctl hw.memsize` + `vm_stat` parsed via `os/exec`; treats free unified memory as available "VRAM" | High | Unified memory architecture — same physical RAM serves CPU and GPU. The "free" number is whatever's free system-wide. |
| Intel Arc — Linux | Linux DRM sysfs | `/sys/class/drm/card*/device/mem_info_vram_*` if exposed by the i915/xe driver | Medium | Newer Intel kernels expose VRAM stats via DRM sysfs; older ones don't. Some Intel discrete GPUs report; integrated ones generally don't. |
| Generic Linux (no GPU) | `/proc/meminfo` | Read `MemAvailable` | High | The honest fallback when no GPU probe finds anything. Treats system RAM as the budget; appropriate when the model is running on CPU. |
| Windows + non-NVIDIA | **none** | — | — | **Honest gap.** No reliable stdlib/os/exec path to AMD or Intel VRAM on Windows without cgo or external tools. Set `--vram-budget=<bytes>` explicitly. |
| Anywhere with override | `override` | Operator-supplied byte count via flag or config | High | Always tried first when set; bypasses all detection. Recommended for CI, containers, or any setup where auto-detection is unreliable. |

## Probe priority order

`vram.Auto()` tries probes in this order and returns the first one that succeeds:

1. **`override`** — if `--vram-budget` or `Config.VRAMBudgetBytes` is set, this wins immediately.
2. **`nvidia-smi`** — most explicit signal where it's available.
3. **`rocm-smi`** — same for AMD.
4. **`darwin-unified`** — only registered on darwin builds.
5. **Linux DRM sysfs** — only registered on linux builds.
6. **`/proc/meminfo`** — only registered on linux builds; the ultimate fallback.

If all probes fail, `vram.Auto()` returns a probe whose `Probe()` reports an explicit "no source detected" error. The runner treats that as "no dynamic cap" — concurrency falls back to the static `MaxConcurrency` value. This is the safe default: nothing breaks, you just don't get VRAM-aware throttling.

## What the runner does when a probe fails mid-run

VRAM is re-read periodically, not per dispatch. A transient probe failure (e.g., `nvidia-smi` got slow) doesn't crash anything — the previous reading is kept until the next refresh succeeds. Long-term sustained failure surfaces in trace logs and falls back to static cap.

## Limitations the probe doesn't address

The probe answers "how much VRAM is currently free." It doesn't answer:

- **What other processes are using VRAM right now.** On NVIDIA you could call `nvidia-smi --query-compute-apps`, but cross-platform that's a nightmare. The probe assumes Oscillitron is the dominant load; if you share the GPU with other workloads, set `--vram-budget` conservatively.
- **What VRAM the model weights occupy after loading.** That's the estimator's job (see `pkg/vram` `SlidingWindowEstimator`), not the probe's. The probe reports free *now*; the estimator predicts future use *per session*.
- **Fragmentation.** A 4 GB-free report doesn't guarantee you can allocate a 4 GB contiguous buffer. The estimator's per-session number includes a conservative overhead margin; if you hit allocation failures despite headroom, increase `BytesPerTokenOverhead` or tighten `MaxConcurrency`.
- **Multi-GPU placement.** The probe aggregates across GPUs by default; per-GPU placement is an inference-server concern, not the orchestrator's. Multi-GPU scheduling is deferred per the parent CLAUDE.md "Hardware parallelism" lock.

## When to use `--vram-budget` directly

- Running in a container with no GPU passthrough (probe reads host VRAM, container can't use it).
- Sharing a GPU with another workload (probe over-reports).
- Windows + AMD/Intel (no auto-detect path; this is the only option).
- CI / dev machines where auto-detection is flaky.
- Smoke tests where determinism matters more than autodetect.

Format: byte count with optional unit suffix. Examples: `--vram-budget=8GB`, `--vram-budget=8589934592`, `--vram-budget=8192MB`.

## Cross-references

- `pkg/vram` package docs — concrete API for callers.
- `references/hermes-persona-slim-down.md` — the persona slim-down work compounds with VRAM-aware throttling; slim prefix → smaller per-session estimate → higher concurrency cap.
- `references/performance-operator-guide.md` — sizing hardware decisions; the coverage gap on Windows+non-NVIDIA bears on the buy-vs-rent decision tree.
