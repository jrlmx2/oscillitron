<!-- CLAUDE GENERATED -->
# TODO: add CI workflow

The hygiene PR (gofmt + git hooks) ships local enforcement only.
The GitHub Actions workflow that runs gofmt + vet + tests as a
merge-gating backstop requires a PAT with `workflow` scope to
push — Claude's default token doesn't have that.

Add this file (one of):

- via the GitHub web UI: Repository → Actions → New workflow → paste
  the YAML below
- by pushing with a PAT that has `workflow` scope

Path: `.github/workflows/check.yml`

```yaml
name: check

on:
  pull_request:
  push:
    branches: [main]

jobs:
  check:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: oscillitron
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: oscillitron/go.mod
          cache: true

      - name: gofmt -l (formatting)
        run: |
          out="$(gofmt -l .)"
          if [ -n "$out" ]; then
            echo "::error::Files need gofmt:"
            echo "$out"
            echo
            echo "Run 'gofmt -w .' from oscillitron/ to fix."
            exit 1
          fi

      - name: go vet
        run: go vet ./...

      - name: go test -race
        run: go test -race ./...
```

Once added, branch protection on `main` should require the `check`
job to pass before merging. CLAUDE.md's "Pre-commit hooks" section
already references the workflow path — once it's added, that doc is
accurate without changes.
