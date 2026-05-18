# [Framework Name TBD] — Design Document

**Status:** Draft v0.2
**Author:** Jim
**Date:** 2026-05-16

> A production-grade compound AI orchestration framework for self-hosted inference, designed to deliver frontier-quality results at a fraction of frontier API cost while providing observability and audit primitives suitable for regulated industries.

---

## 1. Project Context and Intent

This is a personal project, not a venture-backed startup or a team initiative. The form factor and roadmap reflect that reality.

### 1.1 Distribution Model

- **Open-source core, permissively licensed.** The bare-bones orchestrator, session envelope, decomposition/recomposition engine, gateway, and basic observability ship as open source. Anyone can self-deploy, contribute, or fork.
- **Commercial offerings around the core.** Revenue model is consulting, support, custom integrations, and compliance-specific advisory for regulated industries adopting the framework. Possible future commercial features (advanced compliance tooling, managed services, certified deployments) but the bare-bones model stays open.

### 1.2 Why This Structure

- The author has a full-time L6 engineering management role; this cannot be a startup that demands undivided attention.
- The compound AI orchestration space already has well-funded entrants (DSPy, LangGraph, commercial platforms). Competing on framework features as a solo project is unwinnable.
- The author's distinct positioning is **regulated financial services domain experience + technical depth** — monetizable through consulting and selective commercial work, not framework licensing.
- Open source distribution builds credibility, attracts contributors, and seeds the consulting pipeline. The framework is a calling card for the expertise.

### 1.3 Implications for Design

- The OSS core must be genuinely useful standalone, not crippled to push commercial offerings.
- Commercial features should be additive (advanced compliance tooling, certifications, support) rather than restrictions on the core.
- Documentation and reference implementations matter more than feature breadth. Adoption depends on accessibility.
- Scope discipline is essential. Solo maintainership requires saying no to features that don't compound the core value proposition.

---

## 2. Problem Statement

Open-weight models like Qwen3-235B are increasingly competitive with frontier models on many tasks, especially when deployed on purpose-built hardware (DGX Spark, Mac Studio, used A100 nodes). However, structural problems prevent them from being production-ready replacements for frontier APIs:

1. **Quantization quality degradation.** Practical deployments require Q4-level quantization for memory reasons. Naive Q4 introduces measurable quality loss in long reasoning chains.
2. **Long-context attention drift.** Compounding errors over extended thinking sequences hurt quantized models more than full-precision ones.
3. **Capability gap on holistic judgment.** Even calibrated open-weight models trail frontier models on tasks requiring whole-problem integration.

Additionally, **regulated industries face structural barriers** to frontier API adoption that compound AI frameworks generally don't address:

4. **Data residency obligations** (financial services, healthcare, government) prevent routing sensitive data to third-party APIs.
5. **Auditability requirements** demand reproducible, explainable, tamper-evident decision records that frontier APIs typically don't provide.
6. **Model risk management** regimes (SR 11-7 in banking, equivalent in other sectors) require model versioning, reproducibility, and validation evidence.

Existing inference infrastructure (vLLM, SGLang, NIM) solves serving but provides no orchestration. Existing agent frameworks (LangGraph, CrewAI, AutoGen, DSPy) provide orchestration but aren't designed around the failure modes of quantized inference or the compliance needs of regulated industries.

**This framework fills both gaps.** It provides orchestration patterns that make quantized open-weight models viable for production workloads, with observability and compliance primitives suitable for regulated deployment.

---

## 3. Goals and Non-Goals

### Primary Goals

- Provide a Go-based orchestration substrate built on top of vLLM/SGLang/NIM
- Make bounded-session decomposition and recomposition first-class primitives
- Enable multi-model routing between local quantized models and frontier APIs
- Externalize reasoning chains from individual model calls into the orchestration layer
- **Deliver production-acceptable quality at a fraction of frontier API cost** (target: 85% quality at 15% cost on representative workloads)
- Support MCP for context injection and tool access
- Be deployable on custom hardware (DGX Spark, Mac Studio, A100 nodes)

### Secondary Goals (Regulated Industry Defensibility)

- Provide cryptographically signed audit trails for every model interaction
- Enforce data classification and routing policies (regulated data never reaches frontier APIs)
- Capture reproducibility manifests sufficient to replay any decision deterministically
- Support configurable approval gates at high-stakes seams
- Detect and handle PII per policy
- Map framework primitives to common compliance regimes (SR 11-7, SOC 2, HIPAA, GDPR)
- Produce evidence packages exportable for regulatory examination

### Non-Goals

- Not an inference engine. Sits on top of vLLM, SGLang, NIM, or equivalents.
- Not a model fine-tuning framework.
- Not a replacement for LangGraph, CrewAI, or DSPy — though patterns overlap. See Section 12.
- Not a hosted service in the OSS core.
- Not Python-first. Go is the orchestration language.
- Not a GRC platform. Produces compliance evidence; does not manage compliance programs.

---

## 4. Core Architectural Insight

The central insight: **externalize the reasoning chain into the orchestration layer.**

What was historically internal chain-of-thought (a single model call thinking at length) becomes external structured handoffs between many bounded sessions. Quantization cannot degrade reasoning that isn't happening inside a single model call.

A secondary insight that makes the framework compliance-relevant: **externalized reasoning chains are inherently auditable.** Every step is a structured input/output pair in the orchestration layer. Compliance teams can inspect, reproduce, and validate any decision. Monolithic frontier API calls offer no such transparency.

This produces a "many small calls" architecture where:
- Each individual call stays in the model's safe zone for quantization (2K–4K tokens)
- Reasoning happens through structured outputs flowing between sessions
- The orchestrator is the cognition; the model is the substrate
- Frontier API calls are scheduled precisely at the moments they matter
- Every decision leaves a structured, reproducible trail

---

## 5. Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                  User / Application Layer                    │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│            Decomposition Engine + Confirmation Gate          │
│  - Parses prompt, produces structured plan                   │
│  - Surfaces interpretation + open questions to user          │
│  - Captures user attribution on confirmation                 │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                  Go Orchestration Layer                      │
│  - Session dispatch (fan-out)                                │
│  - Session envelope routing (objective/notes/outcome)        │
│  - Notes enrichment between sessions                         │
│  - Model routing (local vs frontier, classification-aware)   │
│  - Recomposition graph execution (tree merges)               │
│  - Data classification enforcement                           │
└──────┬────────────────┬──────────────────┬──────────────────┘
       │                │                  │
       ▼                ▼                  ▼
┌──────────────┐ ┌──────────────┐  ┌──────────────────────────┐
│  Gateway     │ │  MCP Server  │  │  Compliance &            │
│  - Routing   │ │  - Outcomes  │  │  Observability Layer     │
│  - Retries   │ │  - Files     │  │  - Audit ledger          │
│  - Cost track│ │  - Web tools │  │  - Reproducibility       │
└─┬───────┬────┘ └──────────────┘  │  - PII detection         │
  │       │                        │  - Approval gates        │
  ▼       ▼                        │  - Evidence export       │
┌─────┐ ┌──────────┐               │  - Operational traces    │
│vLLM/│ │Frontier  │               └──────────────────────────┘
│SGLng│ │API       │                          │
│local│ │(Claude)  │                          ▼
└─────┘ └──────────┘               ┌──────────────────────────┐
                                   │  Append-only audit store │
                                   │  (cryptographically      │
                                   │   signed, tamper-evident)│
                                   └──────────────────────────┘
```

---

## 6. Component Specifications

### 6.1 The Session Envelope

The atomic unit of work. Every model call is wrapped in this structure.

```json
{
  "session_id": "uuid",
  "session_type": "decompose | analyze | merge | synthesize",
  "objective": "Concrete task framing in one sentence",
  "classification": "public | internal | confidential | regulated",
  "notes": {
    "constraints": [],
    "prior_signals": [],
    "context_tags": []
  },
  "input": {
    "type": "file | page | structured | merge_pair",
    "content": "...",
    "content_hash": "sha256:..."
  },
  "outcome": {
    "verdict": "...",
    "signals": [],
    "confidence": 0.0,
    "open_questions": [],
    "contradictions": [],
    "feeds_into": []
  },
  "routing": {
    "model": "qwen3-235b-awq | claude-sonnet-4",
    "model_hash": "...",
    "reason": "bounded_analysis | complex_synthesis | high_conflict_merge",
    "classification_constraint": "regulated_data_no_frontier"
  },
  "trace": {
    "tokens_input": 0,
    "tokens_output": 0,
    "duration_ms": 0,
    "parent_session": null,
    "cost_usd": 0.0,
    "cost_vs_frontier_baseline_usd": 0.0
  },
  "audit": {
    "ledger_id": "...",
    "signed_at": "...",
    "signature": "..."
  }
}
```

The **classification field** drives routing — regulated-class sessions cannot route to frontier APIs by policy. The **audit block** provides cryptographic anchoring to the audit ledger. The **cost block** tracks actual vs counterfactual frontier cost for ongoing measurement.

### 6.2 Decomposition Engine

Takes a user prompt and produces a structured plan. Surfaces interpretation, requests user confirmation, captures user identity and timestamp on confirmation.

Defaults to local Qwen3. Output schema includes cost estimates at decomposition time, supporting the framework's primary value proposition — users see what they would have paid on frontier APIs vs what this run will cost before executing.

### 6.3 Orchestration Layer (Go)

Built on the goroutine + channel concurrency model. Handles fan-out dispatch and fan-in collection, notes enrichment, classification-aware model routing, retry with classification-respecting fallback (regulated workloads never fall back to frontier on local failure), recomposition graph execution, and audit ledger writes for every state transition.

### 6.4 Notes Enrichment Function

Decides which signals from completed outcomes get injected into the notes field of pending sessions. Initial implementation: tag-based relevance. Future: embedding-based, LLM-classifier for ambiguous cases. Notes enrichment is itself an auditable operation — what was injected into which session is captured in the ledger.

### 6.5 Recomposition Engine

Tree-merge pattern combining structured outcomes. Conflict resolution semantics are deliberately explicit because they double as the audit trail. Compliance reviewers can inspect every conflict, every resolution, every basis for the resolution.

### 6.6 Gateway Layer

LiteLLM-style proxy with framework-aware extensions: unified OpenAI-compatible API, classification-aware routing (regulated → local only), cost tracking with frontier baseline comparison, retry with classification-respecting fallback, built-in observability and audit hooks.

### 6.7 MCP Server

Exposes orchestrator state and tooling via MCP protocol: file reads, outcomes queries, web fetch (classification-aware), code analysis, session context access. MCP tool calls are audit-logged. Some tools may be unavailable for regulated-class sessions.

### 6.8 Compliance & Observability Layer

This is the secondary value proposition's home.

**Audit Ledger** — Append-only, cryptographically signed record of every session, decision, routing choice. Stored separately from operational logs. Tamper-evident (Merkle-tree or equivalent). Suitable for regulatory examination.

**Data Classification Engine** — Tags inputs by sensitivity level. Rule-based + user-tagged initially, ML-based for ambiguous cases later. Classification propagates through the session graph — a regulated input produces regulated downstream sessions.

**Reproducibility Manifest** — Per workflow execution, captures model weight hashes, quantization config, sampling parameters, MCP tool versions, framework version, all session envelopes. Replay possible from manifest alone. Manifest itself is signed and ledger-anchored.

**Approval Gate Primitives** — More general than the decomposition confirmation gate. Configurable seams requiring human approval. Attribution captured. Approval evidence stored in ledger.

**PII Detection and Redaction** — Scans inputs before session dispatch. Configurable per classification level. Detected PII handled per policy: redact, tokenize, block. Integration with existing DLP tooling supported.

**Evidence Export** — Compliance teams export structured evidence packages. "Show me every AI-assisted decision touching customer data in Q3." Includes full reasoning chains, approval records, routing decisions. Format suitable for regulator submission.

**Operational Observability** — Standard Langfuse/Helicone-style traces for developer debugging. Distinct from the audit ledger (different retention, access controls, schema).

---

## 7. Quality Strategy

### 7.1 Output Quality

- Workload-specific AWQ Q4 calibration on user samples
- Bounded session discipline (≤ 4K tokens input context default)
- Multi-model routing sends synthesis and high-conflict merges to frontier APIs where classification permits
- Confirmation gate at decomposition

### 7.2 Cost Quality

- Every workflow tracked with actual cost and frontier baseline cost
- Default routing optimizes for cost-quality frontier, not flexibility
- Cost dashboards and per-workflow reports

### 7.3 Compliance Quality

- Every decision reproducible from the manifest
- Every decision auditable from the ledger
- Every routing choice respects classification
- Every approval gate produces attributable evidence

A workflow that hits output quality but fails cost targets needs different remediation than one that hits cost targets but produces incomplete audit evidence. These dimensions are tracked independently.

---

## 8. Hardware Deployment Targets

**Tier 1: Compact Personal** — DGX Spark (single or paired), Mac Studio M4 Ultra

**Tier 2: Workstation/Lab** — Used A100 80GB nodes, AMD MI300X

**Tier 3: Cloud Operational** — AWS p4d/p5 with vLLM, RunPod / Lambda Labs / Together.ai

**Tier 4: Regulated Enterprise** — On-prem GPU clusters, air-gapped deployments (frontier routing disabled at gateway), FedRAMP-eligible cloud (AWS GovCloud, Azure Government)

---

## 9. Technology Stack

| Layer | Technology | Rationale |
|---|---|---|
| Orchestration | Go | I/O bound, goroutine concurrency, low overhead |
| Data preprocessing (optional) | Rust | CPU-bound throughput |
| Inference (local) | vLLM, SGLang, or NIM | Production-grade, OpenAI-compatible |
| Gateway | LiteLLM (extended) or custom Go | TBD |
| MCP server | Go | Co-located with orchestrator |
| Audit ledger | Append-only KV (BoltDB, Badger) or Postgres + signing | Operational simplicity, integrity |
| Operational observability | Langfuse (self-hosted) | OSS, full trace control |
| Calibration tooling | Python (AutoAWQ, GPTQModel) | Ecosystem reality |
| Evaluation harness | Python or Go | TBD |

---

## 10. Regulatory Framework Mapping

Concrete mapping of framework features to compliance regime requirements. Becomes both a design checklist and a sales tool for consulting engagements.

| Regime | Requirement | Framework Feature |
|---|---|---|
| **SR 11-7 (Banking Model Risk)** | Model inventory and versioning | Reproducibility manifest with weight hashes |
| | Independent validation | Audit ledger enables retrospective validation |
| | Ongoing monitoring | Operational observability + quality eval harness |
| | Documentation | Auto-generated from manifest + ledger |
| **SOC 2 Type II** | Access control evidence | Approval gate attribution + audit ledger |
| | Change management | Framework version pinning in manifest |
| | Incident response | Reproducibility enables forensic replay |
| **HIPAA** | PHI handling | PII detection + classification routing |
| | Audit controls | Audit ledger |
| | Integrity controls | Cryptographic signing of ledger entries |
| **GDPR** | Right to explanation | Externalized reasoning enables post-hoc explanation |
| | Data minimization | Classification routing limits frontier API exposure |
| | Data residency | Self-hosted inference + classification enforcement |
| **SOX (Internal Controls)** | Reproducibility | Reproducibility manifest |
| | Segregation of duties | Approval gate with attribution |
| | Audit trail | Audit ledger |
| **FedRAMP** | Continuous monitoring | Operational observability |
| | Configuration management | Framework + model + manifest versioning |

These mappings need validation by qualified counsel and compliance professionals before being used as compliance claims. The framework provides technical primitives; certified compliance is the customer's responsibility (or the consulting engagement's deliverable).

---

## 11. Business Model

### 11.1 Open Source Core

- Permissive license (Apache 2.0 leading, MIT possible)
- Bare-bones orchestrator, session envelope, decomposition/recomposition, gateway, basic MCP, basic audit ledger
- Self-deployable, contributable, forkable
- No artificial limits on the OSS core

### 11.2 Commercial Offerings

Revenue is consulting and selective commercial work, not framework licensing.

**Consulting Services**
- Compound AI architecture advisory for regulated industries
- Migration consulting (frontier API to hybrid local + frontier)
- Compliance evidence package design and review
- Custom workflow development on the framework
- Calibration engagements (workload-specific quantization tuning)

**Commercial Add-ons (Future, Optional)**
- Certified deployment templates (FedRAMP, HIPAA-ready configurations)
- Managed audit ledger service with regulator-friendly retention guarantees
- Advanced compliance evidence tooling
- Priority support and SLA-backed assistance
- Long-term reproducibility hosting (model weights + manifest archive)

**Principle: commercial offerings are additive to the OSS core, never restrictions on it.** The framework should be genuinely useful and complete as open source. Commercial value comes from expertise, certification, and managed services around it.

### 11.3 Why This Works for a Solo Project

- Open source distribution requires no sales motion or customer success org
- Consulting engagements are high-margin, finite in scope, can be sized to fit alongside a full-time role
- The framework itself is the lead generator
- Regulated industry buyers pay for accountability and expertise, not framework features
- The author's existing financial services context provides credibility and pipeline

---

## 12. Competitive Landscape

The decomposition + recomposition + multi-model routing pattern is **not novel**. It's an instance of "Compound AI Systems," a paradigm formally articulated by UC Berkeley AI Research in February 2024 and now the dominant production AI architecture. Honest positioning matters.

### 12.1 Compound AI Paradigm

Position within the compound AI paradigm rather than as a new category. Relevant prior art:
- Berkeley BAIR compound AI blog (Feb 2024) — the conceptual foundation
- Branch-Solve-Merge (Meta) — pairwise decomposition + merge
- Tree-of-Thoughts — branching reasoning with synthesis
- Componentization / MAODchat (Sept 2025) — closest direct prior art; explicitly formalizes decomposition + recomposition stages
- ACONIC (Oct 2025) — constraint-based decomposition
- Recursive Language Models — lambda-calculus inspired recursive invocation

### 12.2 Adjacent Production Frameworks

| Framework | Focus | Distinction from This Framework |
|---|---|---|
| **DSPy** (Stanford) | Programming-with-LLMs, compiler-style optimization | Python; not focused on quantized inference or compliance |
| **LangGraph** | Graph-based agent workflows | Python; workflow-flexibility-first; no compliance primitives |
| **Haystack** | Production pipelines | Python; mature; not opinionated about self-hosted quantized inference |
| **CrewAI** | Role-based agent abstraction | Different abstraction; not graph-based |
| **AutoGen** (Microsoft) | Multi-agent conversation | Conversational; not bounded-session graphs |
| **Chains** (Baseten) | Compound AI on Baseten infrastructure | Hosted; not self-host-first |
| **TrueFoundry** | Compound AI platform | Commercial platform; not OSS-first |

### 12.3 Compliance / Observability Platforms

| Platform | Focus | Distinction |
|---|---|---|
| **Credo AI, Holistic AI, Fiddler** | AI governance and monitoring | Layer above the framework; this produces evidence they consume |
| **Langfuse, Phoenix, Helicone** | LLM observability | This includes operational observability; adds compliance-grade audit ledger as distinct layer |

### 12.4 The Distinct Position

> An opinionated open-source compound AI framework for self-hosted quantized inference, designed for cost efficiency and regulatory defensibility. Built in Go on top of vLLM/SGLang/NIM. Targets the cost-conscious, compliance-sensitive production user — a segment underserved by Python-first, cloud-API-first, flexibility-maximizing alternatives.

**What's defensible:**
- Go-native on self-hosted inference (rare combination)
- Quality-preservation-first design for quantized models
- Compliance and audit primitives as first-class architecture
- Hardware tier opinionation for prosumer-to-small-enterprise deployments
- Solo-maintainer-sustainable scope discipline

**What's not novel and shouldn't be claimed:**
- The compound AI architecture pattern
- Decomposition + recomposition as a strategy
- Multi-model routing
- MCP integration

---

## 13. Open Items and Research Questions

### 13.1 Quality and Quantization
- Empirical session-length sweet spot per task type
- Workload-specific calibration data collection pipeline (with privacy implications)
- Quality regression detection harness
- Mixed-precision per-layer policies

### 13.2 Notes Enrichment
- Tag taxonomy design (controlled vs emergent)
- Relevance thresholds
- Embedding-based vs tag-based tradeoffs
- Notes pruning to prevent bloat

### 13.3 Recomposition
- Merge ordering sensitivity
- Conflict counting heuristics for frontier escalation
- Unresolved conflict propagation strategy
- Cross-schema outcome merging

### 13.4 Irreducible Task Taxonomy
The empirical work that most validates the framework's primary value proposition.

### 13.5 Routing Logic
- Cost-aware vs latency-aware vs quality-aware routing tradeoffs
- Per-workflow quality budgets
- Classification-driven routing enforcement

### 13.6 Decomposition
- Domain-specific decomposition prompt library
- Recursive decomposition for oversized sub-tasks
- Decomposition quality measurement without full pipeline run

### 13.7 MCP
- Tool authorization model
- Per-classification tool availability rules
- Tool result caching across sessions

### 13.8 Compliance and Observability
- **Audit ledger schema and integrity model.** Merkle tree vs hash chain vs blockchain anchoring tradeoffs.
- **Cryptographic signing key management.** Per-deployment, per-tenant, hardware-backed (HSM/TPM) options.
- **Reproducibility manifest contents.** Minimum sufficient set for replay; handling non-deterministic sampling.
- **Data classification taxonomy.** Standardize core levels, allow extension.
- **PII detection accuracy.** Rule-based vs ML-based tradeoffs.
- **Approval gate UX patterns.** CLI, web, integration with existing approval systems.
- **Regulatory mapping validation.** Each row in Section 10 needs validation by qualified compliance counsel.
- **Evidence export formats.** What do regulators actually want? Empirical research with compliance officers.
- **GRC platform integration.** Where does framework evidence land in existing tooling?

### 13.9 Deployment
- Single-node vs multi-node orchestrator
- State persistence backend defaults
- Multi-tenant mode design
- Air-gapped deployment patterns for regulated tier

### 13.10 Project Governance
- License choice
- Repo structure
- Naming
- Maintainer model
- Documentation strategy
- Contribution policy

---

## 14. Roadmap

Paced to a solo maintainer with a full-time role. Phases may take months each. Each phase produces a usable system.

### Phase 0 — Foundation (current)
Design doc ratified, hardware decision, initial repo/license/naming, one workflow chosen for empirical validation.

### Phase 1 — Empirical Validation
Manual implementation on chosen workflow. Measure cost ratio and quality delta vs frontier baseline. **Decision gate:** does the architecture deliver on the cost-quality goal? Kill or proceed honestly.

### Phase 2 — Minimum Viable Orchestrator
Go orchestrator with session envelope (including classification, audit hooks). vLLM integration. Fan-out/fan-in. File-per-session reference implementation. Basic operational traces.

### Phase 3 — Decomposition + Confirmation
Decomposition engine with cost estimation. CLI confirmation UX. Tag-based notes enrichment.

### Phase 4 — Multi-Model Routing + Audit Ledger
Gateway with classification-aware routing. Frontier API integration. Audit ledger with cryptographic signing. Reproducibility manifest collection.

### Phase 5 — Recomposition
Tree merge. Conflict-resolution prompts. High-conflict routing escalation.

### Phase 6 — MCP and Compliance Primitives
MCP server with core tools. PII detection. Approval gate primitives. Evidence export tooling.

### Phase 7 — Quality and Calibration Tooling
One-command AWQ calibration on user samples. Quality eval harness. Cost reporting dashboards.

### Phase 8 — Production Hardening
Persistence options. Multi-tenancy. Authorization model. Air-gapped templates. Regulatory mapping validation with counsel.

### Phase 9 — Consulting Practice
First consulting engagements. Case studies. Pipeline development.

---

## 15. Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Phase 1 validation shows the cost-quality math doesn't work | Kill the project honestly; pivot insights to consulting on existing frameworks |
| Open-weight model quality improves to where compound architecture isn't justified | Framework still useful for compliance and observability |
| Frontier API costs drop dramatically | Hybrid routing remains valuable; compliance value unchanged |
| Quantization techniques improve to near-lossless Q4 | Framework gets simpler; cost-quality goal easier |
| MCP doesn't reach adoption | Tool-use abstraction is portable to other protocols |
| Solo maintainer time crunch | Open contribution model; ruthless scope discipline; framework usable at each phase |
| Regulated industry adoption requires certifications author can't provide solo | Consulting can deliver certified per-customer; OSS core uncertified |
| Adjacent framework adds the distinct features (Go bindings, compliance) | Author's regulated industry domain experience remains the durable advantage |

---

## 16. Decisions Pending

- [ ] Framework name
- [ ] License (Apache 2.0 recommended)
- [ ] Phase 1 validation workflow (code analysis recommended for publishable evidence)
- [ ] Hardware commitment (DGX Spark pair leading)
- [ ] Whether to extend LiteLLM or build the gateway in Go
- [ ] Audit ledger storage backend
- [ ] Confirmation gate UX target (CLI first)
- [ ] Initial public announcement timing (after Phase 2 minimum)

---

## Appendix A: Key Conversation Insights

1. Quality degradation in quantized models is concentrated in long-context reasoning chains, not per-token output. Bounded sessions are a viable mitigation.
2. Larger MoE models are more robust to quantization than smaller dense models.
3. User confirmation at the decomposition seam is a high-leverage quality and compliance gate.
4. The natural grain of the problem domain produces better session boundaries than arbitrary chunking.
5. Recomposition is the under-appreciated half; pairwise/tree merging with explicit conflict resolution is where significant quality lives.
6. The orchestrator is the cognition; the model is the substrate. Externalized reasoning enables both quality preservation and auditability.
7. Frontier API routing should be precisely scheduled, not avoided. The hybrid wins by spending API tokens at exactly the moments they earn their cost.
8. Calibrated Q4 with workload-specific data significantly outperforms generic Q4.
9. Externalized reasoning chains are inherently more auditable than monolithic frontier API calls. This bridges the primary and secondary value propositions.
10. The compound AI pattern is not novel — but its specific implementation for self-hosted quantized inference with compliance primitives is underserved.
11. As a solo project, scope discipline matters more than feature breadth. The framework's value is in opinionation, not flexibility.
12. The consulting business model converts framework adoption into income without requiring framework licensing or a sales motion.

---

*End of design doc v0.2. Open for revision.*
