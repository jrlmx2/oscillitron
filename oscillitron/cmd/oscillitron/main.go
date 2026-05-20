// CLAUDE GENERATED
// Demo runner — placeholder during the uniform-node refactor.
//
// Stage 6 of the refactor (parent CLAUDE.md "Architecture",
// scratch/design-notes.md "JSON envelope sketch") rewrites this onto
// the new shape: a root AP whose evaluate picks plan; plan emits N
// process sub-APs with a recompose spec; the runner walks the call
// tree synchronously with randomized sibling dispatch; compose pulls
// from the parent scope channel; critique fires per the verifier
// policy.
//
// Until Stage 6 lands, this binary just prints a status line so
// `go run ./cmd/oscillitron` doesn't break the build.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "oscillitron demo: rewrite pending (Stage 6 of uniform-node refactor).")
	fmt.Fprintln(os.Stderr, "See scratch/design-notes.md \"JSON envelope sketch\" and parent CLAUDE.md.")
}
