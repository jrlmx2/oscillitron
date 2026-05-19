#!/usr/bin/env bash
# CLAUDE GENERATED
#
# Set up a local model backend for Hermes (Oscillitron's wrapped specialist
# substrate) across OS / backend / model combinations. Handles the
# Hermes-specific gotchas surfaced during the first integration:
#
#   - Hermes requires ≥64K context window on the main model AND on every
#     auxiliary subsystem (compression / summarization / memory). Each
#     gets its own config knob; all four must be ≥64K or session/new
#     fails with internal error -32603.
#   - Models like qwen2.5-coder:14b expose 32K by default via Ollama
#     even though the model supports 128K with YaRN. This script builds
#     a Modelfile-derived tag with the model's real native context so
#     Hermes sees the true window — not a lie that Hermes-then-Ollama
#     silently truncates against.
#   - For "custom" provider, Hermes' router falls back to OpenRouter
#     unless OPENAI_BASE_URL + OPENAI_API_KEY are also exported.
#
# Usage:
#   ./scripts/setup-hermes-backend.sh                # interactive: detect + recommend, then run all
#   ./scripts/setup-hermes-backend.sh detect         # print HW/OS/backend report
#   ./scripts/setup-hermes-backend.sh install        # install the backend
#   ./scripts/setup-hermes-backend.sh pull           # pull model + build extended-context variant
#   ./scripts/setup-hermes-backend.sh config         # write ~/.hermes/config.yaml
#   ./scripts/setup-hermes-backend.sh verify         # health-check server + model
#   ./scripts/setup-hermes-backend.sh all            # detect, install, pull, config, verify
#
# Env (override defaults; otherwise interactive prompts or detection):
#   HERMES_BACKEND     ollama | lmstudio | vllm | custom
#   HERMES_MODEL       backend-specific model identifier
#   HERMES_NATIVE_CTX  the model's real native context window (tokens)
#   HERMES_BASE_URL    OpenAI-compatible endpoint URL
#   HERMES_API_KEY     API key (use a dummy for local servers that don't need auth)
#   HERMES_CONFIG      path to hermes config.yaml (default: ~/.hermes/config.yaml)

set -euo pipefail

HERMES_CONFIG="${HERMES_CONFIG:-$HOME/.hermes/config.yaml}"
HERMES_VENV="${HERMES_VENV:-$HOME/hermes-agent/venv}"
HERMES_MIN_CTX=65536  # Hermes' hard minimum

# Known-good model registry. Case statement (Bash 3.2-compatible — macOS
# stock bash, no associative arrays). Echoes the model's REAL native
# context window in tokens — not Ollama's default num_ctx (usually
# clamped to 32K). Empty echo means "unknown; caller should require
# HERMES_NATIVE_CTX explicitly."
model_native_ctx() {
  case "$1" in
    qwen2.5-coder:7b-instruct-q6_K)      echo 131072 ;;
    qwen2.5-coder:14b-instruct-q6_K)     echo 131072 ;;
    qwen2.5-coder:32b-instruct-q3_K_M)   echo 131072 ;;
    qwen2.5-coder:14b)                   echo 131072 ;;
    qwen2.5-coder:7b)                    echo 131072 ;;
    llama3.1:8b)                         echo 131072 ;;
    llama3.1:8b-instruct-q6_K)           echo 131072 ;;
    mistral-nemo:12b)                    echo 131072 ;;
    deepseek-coder-v2:16b)               echo 163840 ;;
    gemma2:9b)                           echo 8192   ;;  # only 8K — won't satisfy Hermes
    gpt-oss:20b)                         echo 131072 ;;
    *)                                   echo ""     ;;
  esac
}

log()  { printf '\033[1;36m[hermes-backend]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[hermes-backend]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[hermes-backend]\033[0m %s\n' "$*" >&2; exit 1; }

# ─── Detection ───────────────────────────────────────────────────────────

detect_os() {
  local s
  s="$(uname -s)"
  case "$s" in
    Darwin)            echo "darwin" ;;
    Linux)
      if grep -qi microsoft /proc/version 2>/dev/null; then echo "wsl"
      else                 echo "linux"; fi ;;
    MINGW*|CYGWIN*|MSYS*) echo "windows" ;;
    *)                   echo "$s" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    arm64|aarch64) echo "arm64" ;;
    x86_64|amd64)  echo "amd64" ;;
    *)             echo "$(uname -m)" ;;
  esac
}

detect_ram_gb() {
  local os
  os="$(detect_os)"
  case "$os" in
    darwin)
      sysctl -n hw.memsize | awk '{printf "%.0f", $1/1024/1024/1024}' ;;
    linux|wsl)
      awk '/MemTotal/ {printf "%.0f", $2/1024/1024}' /proc/meminfo ;;
    *) echo "?" ;;
  esac
}

detect_gpu() {
  local os
  os="$(detect_os)"
  if [ "$os" = "darwin" ]; then
    # M-series chips have a unified-memory GPU; surface its name.
    system_profiler SPDisplaysDataType 2>/dev/null | awk -F': ' '/Chipset Model/ {print $2; exit}'
    return
  fi
  if command -v nvidia-smi >/dev/null 2>&1; then
    nvidia-smi --query-gpu=name --format=csv,noheader | head -1
    return
  fi
  if command -v rocm-smi >/dev/null 2>&1; then
    echo "ROCm device present"
    return
  fi
  echo "none"
}

detect_disk_free_gb() {
  df -k "$HOME" | awk 'NR==2 {printf "%.0f", $4/1024/1024}'
}

recommend_backend() {
  local os gpu
  os="$(detect_os)"
  gpu="$(detect_gpu)"
  if [ "$os" = "linux" ] && [[ "$gpu" == *NVIDIA* || "$gpu" == *Tesla* || "$gpu" == *RTX* ]]; then
    echo "vllm"   # best perf on Linux+CUDA
  else
    echo "ollama" # works everywhere with minimal setup
  fi
}

cmd_detect() {
  local os arch ram disk gpu rec
  os="$(detect_os)"; arch="$(detect_arch)"
  ram="$(detect_ram_gb)"; disk="$(detect_disk_free_gb)"
  gpu="$(detect_gpu)"; rec="$(recommend_backend)"
  cat <<EOF
[hermes-backend] system:
  OS:           $os ($arch)
  RAM:          ${ram} GB
  GPU:          $gpu
  Disk free:    ${disk} GB on \$HOME
  Recommended:  $rec

Set HERMES_BACKEND=$rec (or override) and re-run with: install / pull / config / verify / all
EOF
}

# ─── Resolve choices (env > prompt > recommendation) ─────────────────────

resolve_backend() {
  if [ -n "${HERMES_BACKEND:-}" ]; then echo "$HERMES_BACKEND"; return; fi
  recommend_backend
}

resolve_model() {
  if [ -n "${HERMES_MODEL:-}" ]; then echo "$HERMES_MODEL"; return; fi

  # RAM-aware default. Hermes' 64K floor means the KV cache costs are
  # non-trivial — a 14B Q6 model at 64K context allocates ~20 GB total
  # (weights + KV cache), which spills past 18 GB unified RAM and turns
  # inference into a swap-fest. Smaller models earn their keep here.
  #
  # Rules of thumb on Apple Silicon / typical dev hardware at 64K ctx:
  #   <16 GB RAM: too small to run Hermes-grade local — recommend cloud.
  #   16-24 GB:   qwen2.5-coder:7b Q6 (~9 GB resident)
  #   24-48 GB:   qwen2.5-coder:14b Q6 (~20 GB resident, fits comfortably)
  #   48+ GB:     qwen2.5-coder:32b Q3_K_M (~22 GB) or larger ctx with 14b
  local ram_gb
  ram_gb="$(detect_ram_gb)"
  local default_ollama
  if   [ "$ram_gb" = "?" ] || (( ram_gb < 16 )); then
    default_ollama="qwen2.5-coder:7b-instruct-q6_K"
    warn "RAM=${ram_gb}GB is tight for Hermes' 64K context floor — consider cloud or a smaller model"
  elif (( ram_gb < 24 )); then
    default_ollama="qwen2.5-coder:7b-instruct-q6_K"
  elif (( ram_gb < 48 )); then
    default_ollama="qwen2.5-coder:14b-instruct-q6_K"
  else
    default_ollama="qwen2.5-coder:32b-instruct-q3_K_M"
  fi

  case "$1" in
    ollama)   echo "$default_ollama" ;;
    lmstudio) echo "qwen2.5-coder-14b-instruct-mlx" ;;  # GUI-managed; size is user's call
    vllm)     echo "Qwen/Qwen2.5-Coder-14B-Instruct" ;;  # Linux+GPU, size less constrained
    *)        echo "" ;;
  esac
}

resolve_native_ctx() {
  # If the user explicitly set the cap, honor it.
  if [ -n "${HERMES_NATIVE_CTX:-}" ]; then echo "$HERMES_NATIVE_CTX"; return; fi

  # Otherwise pick the smallest value that satisfies Hermes (≥64K),
  # bounded by the model's true native max. Going to the model's full
  # native max sounds appealing but allocates KV cache linearly: a 128K
  # context on a 14B Q6 model needs ~7 GB of KV cache, which is fatal
  # on 18 GB RAM machines. Bigger context only earns its weight when
  # the workload actually fills it. Override with HERMES_NATIVE_CTX
  # when you've got the RAM and the use case.
  local max
  max="$(model_native_ctx "$1")"
  if [ -z "$max" ]; then
    echo "$HERMES_MIN_CTX"; return
  fi
  if (( max < HERMES_MIN_CTX )); then
    echo "$max"  # caller will refuse if this is below the floor
  else
    echo "$HERMES_MIN_CTX"
  fi
}

# ─── Per-backend: install ────────────────────────────────────────────────

install_ollama() {
  if command -v ollama >/dev/null 2>&1; then
    log "ollama already installed at: $(command -v ollama)"
  else
    local os; os="$(detect_os)"
    case "$os" in
      darwin)
        command -v brew >/dev/null 2>&1 || die "brew not found. Install Homebrew (https://brew.sh) first."
        log "brew install ollama"
        brew install ollama ;;
      linux|wsl)
        log "curl -fsSL https://ollama.com/install.sh | sh"
        curl -fsSL https://ollama.com/install.sh | sh ;;
      *) die "ollama install not scripted for OS=$os — install manually from https://ollama.com" ;;
    esac
  fi
  # Start daemon if not already serving.
  if curl -sf -m 2 http://127.0.0.1:11434/api/tags >/dev/null 2>&1; then
    log "ollama daemon already running"
    return 0
  fi
  log "starting 'ollama serve' in background (log: ~/.ollama/serve.log)"
  mkdir -p "$HOME/.ollama"
  nohup ollama serve > "$HOME/.ollama/serve.log" 2>&1 &
  disown || true
  local i
  for i in $(seq 1 20); do
    if curl -sf -m 1 http://127.0.0.1:11434/api/tags >/dev/null 2>&1; then
      log "ollama up after ${i}s"; return 0
    fi
    sleep 1
  done
  die "ollama daemon did not come up — check ~/.ollama/serve.log"
}

install_lmstudio() {
  if curl -sf -m 2 http://127.0.0.1:1234/v1/models >/dev/null 2>&1; then
    log "lmstudio server is reachable on http://127.0.0.1:1234"
    return 0
  fi
  cat <<'EOF'
[hermes-backend] LM Studio is a GUI app — no CLI install path. Manual setup:
  1. Download from https://lmstudio.ai
  2. Open LM Studio → Search → download a model with ≥64K context
     (qwen2.5-coder-14b-instruct-mlx recommended on Apple Silicon)
  3. Local Server tab → load the model → Start Server (port 1234)
  4. In Settings → Server → set "Context Length" to the model's native max
     (NOT the Hermes minimum — the model's actual capability).
  5. Re-run: ./scripts/setup-hermes-backend.sh verify
EOF
  return 1
}

install_vllm() {
  local os gpu
  os="$(detect_os)"; gpu="$(detect_gpu)"
  if [ "$os" != "linux" ] && [ "$os" != "wsl" ]; then
    cat <<EOF
[hermes-backend] vLLM is Linux-only (NVIDIA recommended). Detected OS=$os, GPU=$gpu.
  - On Apple Silicon, use Ollama (Metal-accelerated) or MLX via LM Studio.
  - On Windows, use WSL2 + this script.
EOF
    return 1
  fi
  if ! command -v python3 >/dev/null 2>&1; then
    die "python3 not found — install Python 3.10+ before vLLM"
  fi
  log "installing vLLM into ~/.oscillitron-vllm-venv (one-time, ~3GB)"
  python3 -m venv "$HOME/.oscillitron-vllm-venv"
  # shellcheck source=/dev/null
  source "$HOME/.oscillitron-vllm-venv/bin/activate"
  pip install --quiet --upgrade pip
  pip install --quiet vllm
  log "vLLM installed. Launch with:"
  cat <<EOF
    source ~/.oscillitron-vllm-venv/bin/activate
    python -m vllm.entrypoints.openai.api_server \\
        --model $(resolve_model vllm) \\
        --max-model-len $(resolve_native_ctx "$(resolve_model vllm)") \\
        --port 8000
EOF
}

cmd_install() {
  local backend; backend="$(resolve_backend)"
  log "installing backend: $backend"
  case "$backend" in
    ollama)   install_ollama ;;
    lmstudio) install_lmstudio ;;
    vllm)     install_vllm ;;
    custom)   log "custom backend — nothing to install. Ensure your endpoint is reachable at HERMES_BASE_URL." ;;
    *)        die "unknown backend: $backend" ;;
  esac
}

# ─── Per-backend: pull / configure backend-side ──────────────────────────

# For Ollama, build a derived tag with PARAMETER num_ctx set to the model's
# real native context. This is the principled fix for Hermes' 64K floor:
# instead of lying to Hermes via config_length, we make Ollama actually
# serve the larger window the model supports.
extended_ollama_tag() {
  local base="$1"
  # Replace ':' with '-' for the derived name. e.g.
  # qwen2.5-coder:14b-instruct-q6_K -> qwen2.5-coder-14b-instruct-q6_K-osc
  echo "${base//:/-}-osc"
}

pull_ollama() {
  local model native_ctx ext
  model="$(resolve_model ollama)"
  native_ctx="$(resolve_native_ctx "$model")"
  ext="$(extended_ollama_tag "$model")"

  if (( native_ctx < HERMES_MIN_CTX )); then
    warn "model $model native context ($native_ctx) is below Hermes minimum ($HERMES_MIN_CTX). Pick another model."
    return 1
  fi

  # Pull the base model if missing.
  if ollama list 2>/dev/null | awk '{print $1}' | grep -qx "$model"; then
    log "base model already present: $model"
  else
    log "pulling $model (one-time)"
    ollama pull "$model"
  fi

  # Build the extended-context variant if missing.
  if ollama list 2>/dev/null | awk '{print $1}' | grep -qx "${ext}:latest"; then
    log "extended-context variant already exists: ${ext}"
    return 0
  fi
  local mf
  mf="$(mktemp)"
  cat > "$mf" <<EOF
FROM $model
PARAMETER num_ctx $native_ctx
EOF
  log "building $ext with num_ctx=$native_ctx (Modelfile from $model)"
  ollama create "$ext" -f "$mf"
  rm -f "$mf"
}

cmd_pull() {
  local backend; backend="$(resolve_backend)"
  case "$backend" in
    ollama)   pull_ollama ;;
    lmstudio) log "lmstudio: model selection happens in the GUI. Verify with 'verify'." ;;
    vllm)     log "vllm: model is downloaded automatically when the server starts (HuggingFace cache)." ;;
    custom)   log "custom: model setup is your responsibility." ;;
    *)        die "unknown backend: $backend" ;;
  esac
}

# ─── Hermes config writer ────────────────────────────────────────────────

resolve_base_url() {
  if [ -n "${HERMES_BASE_URL:-}" ]; then echo "$HERMES_BASE_URL"; return; fi
  case "$1" in
    ollama)   echo "http://127.0.0.1:11434/v1" ;;
    lmstudio) echo "http://127.0.0.1:1234/v1" ;;
    vllm)     echo "http://127.0.0.1:8000/v1" ;;
    *)        echo "" ;;
  esac
}

resolve_api_key() {
  if [ -n "${HERMES_API_KEY:-}" ]; then echo "$HERMES_API_KEY"; return; fi
  case "$1" in
    ollama)   echo "ollama" ;;
    lmstudio) echo "lm-studio" ;;
    vllm)     echo "vllm" ;;
    *)        echo "no-key-required" ;;
  esac
}

# Resolve the "effective" model name we send to Hermes (i.e., the
# extended-context tag for Ollama, not the bare base tag).
resolve_effective_model() {
  local backend="$1" base="$2"
  case "$backend" in
    ollama) extended_ollama_tag "$base" ;;
    *)      echo "$base" ;;
  esac
}

# Hermes-side provider name (Hermes config.yaml `model.provider`).
hermes_provider() {
  case "$1" in
    ollama)   echo "custom" ;;       # "ollama" is documented as an alias but config.yaml silently drops it
    lmstudio) echo "lmstudio" ;;     # first-class in Hermes
    vllm)     echo "custom" ;;
    custom)   echo "custom" ;;
  esac
}

cmd_config() {
  local backend model native_ctx eff_model base_url api_key provider
  backend="$(resolve_backend)"
  model="$(resolve_model "$backend")"
  native_ctx="$(resolve_native_ctx "$model")"
  eff_model="$(resolve_effective_model "$backend" "$model")"
  base_url="$(resolve_base_url "$backend")"
  api_key="$(resolve_api_key "$backend")"
  provider="$(hermes_provider "$backend")"

  if (( native_ctx < HERMES_MIN_CTX )); then
    die "native_ctx=$native_ctx is below Hermes minimum ($HERMES_MIN_CTX). Pick a different model or set HERMES_NATIVE_CTX manually."
  fi

  [ -f "$HERMES_CONFIG" ] || die "Hermes config not found at $HERMES_CONFIG (run scripts/setup-hermes-local.sh setup first)"
  [ -x "$HERMES_VENV/bin/python3" ] || die "Hermes venv python not found at $HERMES_VENV/bin/python3"

  log "writing $HERMES_CONFIG (backup -> ${HERMES_CONFIG}.bak)"
  cp "$HERMES_CONFIG" "${HERMES_CONFIG}.bak"

  CONFIG="$HERMES_CONFIG" \
  MODEL_NAME="$eff_model" \
  PROVIDER="$provider" \
  BASE_URL="$base_url" \
  API_KEY="$api_key" \
  CTX_LEN="$native_ctx" \
    "$HERMES_VENV/bin/python3" <<'PY'
import os, yaml

cfg_path = os.environ["CONFIG"]
with open(cfg_path) as fh:
    cfg = yaml.safe_load(fh) or {}

# Main model: object form. Hermes requires .provider in its canonical
# name ("custom" or "lmstudio"); aliases like "ollama" are silently
# dropped and the auxiliary router falls back to OpenRouter.
cfg["model"] = {
    "default":        os.environ["MODEL_NAME"],
    "provider":       os.environ["PROVIDER"],
    "base_url":       os.environ["BASE_URL"],
    "api_key":        os.environ["API_KEY"],
    "context_length": int(os.environ["CTX_LEN"]),
}

# Every auxiliary subsystem (compression / summarization / memory)
# inherits the main model by default AND enforces the >=64K floor
# independently. Each gets the same context_length.
aux = cfg.get("auxiliary") or {}
for sub in ("compression", "summarization", "memory"):
    s = aux.get(sub) or {}
    s["context_length"] = int(os.environ["CTX_LEN"])
    aux[sub] = s
cfg["auxiliary"] = aux

with open(cfg_path, "w") as fh:
    yaml.safe_dump(cfg, fh, sort_keys=False, default_flow_style=False)

print(f"config: model={os.environ['MODEL_NAME']}, provider={os.environ['PROVIDER']}, "
      f"base_url={os.environ['BASE_URL']}, context_length={os.environ['CTX_LEN']}")
PY

  # Regenerate the hermes-acp wrapper so OPENAI_BASE_URL / OPENAI_API_KEY
  # reflect this backend. The wrapper path matches setup-hermes-local.sh.
  local wrapper="${HERMES_WRAPPER:-$HOME/bin/hermes-acp}"
  local hermes_bin="$HOME/hermes-agent/venv/bin/hermes"
  if [ -x "$hermes_bin" ]; then
    mkdir -p "$(dirname "$wrapper")"
    cat > "$wrapper" <<EOF
#!/usr/bin/env bash
# Auto-generated by oscillitron/scripts/setup-hermes-backend.sh
export HERMES_INFERENCE_PROVIDER=$provider
export OPENAI_BASE_URL=$base_url
export OPENAI_API_KEY=$api_key
EOF
    if [ "$backend" = "ollama" ]; then
      printf 'export OLLAMA_BASE_URL=http://127.0.0.1:11434\n' >> "$wrapper"
    fi
    printf 'exec "%s" acp "$@"\n' "$hermes_bin" >> "$wrapper"
    chmod +x "$wrapper"
    log "wrapper written: $wrapper"
  else
    warn "Hermes binary not found at $hermes_bin — skipping wrapper write (run setup-hermes-local.sh first)"
  fi
}

# ─── Verify ──────────────────────────────────────────────────────────────

cmd_verify() {
  local backend; backend="$(resolve_backend)"
  local base_url; base_url="$(resolve_base_url "$backend")"
  log "verify: backend=$backend base_url=$base_url"

  if ! curl -sf -m 5 "$base_url/models" >/dev/null 2>&1; then
    die "backend not reachable at $base_url — run 'install' or start the server manually"
  fi
  log "✓ backend reachable"

  if [ "$backend" = "ollama" ]; then
    local model eff
    model="$(resolve_model ollama)"
    eff="$(extended_ollama_tag "$model")"
    if ! ollama list | awk '{print $1}' | grep -qx "${eff}:latest"; then
      warn "extended-context tag ${eff} not present — run 'pull'"
      return 1
    fi
    log "✓ extended-context tag present: $eff"
  fi

  log "verification passed. Try: ./scripts/setup-hermes-local.sh test"
}

# ─── Dispatch ────────────────────────────────────────────────────────────

mode="${1:-all}"
case "$mode" in
  detect)  cmd_detect ;;
  install) cmd_install ;;
  pull)    cmd_pull ;;
  config)  cmd_config ;;
  verify)  cmd_verify ;;
  all)
    cmd_detect
    cmd_install
    cmd_pull
    cmd_config
    cmd_verify
    log "done. Next: ./scripts/setup-hermes-local.sh test"
    ;;
  *) die "unknown mode: $mode (expected one of: detect, install, pull, config, verify, all)" ;;
esac
