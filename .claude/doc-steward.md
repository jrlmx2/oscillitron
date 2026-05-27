# Doc Steward Concerns

## CLAUDE.md
- Architecture decisions: locked items must match code reality in oscillitron/
- Status section: stage label, version number, what's complete vs in-progress
- Open questions list: resolved items should migrate to "Recently locked" with date
- Stack & tooling: Go version, module path, dependencies

## oscillitron/CLAUDE.md
- Package inventory: every pkg/* directory should be listed with a one-line description
- Test commands: `go test` invocations must work as documented
- Build commands: `go build` targets must be current

## INDEX.md
- Every file in references/ and skills/ has an entry
- Hook descriptions (when-to-load) match actual file contents

## scratch/design-notes.md
- Section headings match concepts still in use (renamed/removed concepts = stale section)
- Code examples reference real types and function signatures

## references/phase1-measurement-guide.md
- Measurement approach matches cmd/phase1 implementation
- Case categories match cases.json entries
