package artifact

import (
	"fmt"
	"os"
	"testing"
)

func TestDebugLoose(t *testing.T) {
	data, err := os.ReadFile("testdata/real_loose_output.md")
	if err != nil { t.Skip() }
	body, ok := extractBlock(string(data))
	fmt.Println("extract ok:", ok, "len:", len(body))
	files, man := parseLooseArtifacts(body)
	fmt.Println("files:", len(files), "manifest files:", len(man.Files), "verify:", len(man.Verify))
	for _, f := range files { fmt.Println("  file:", f.Path, len(f.Body)) }
	r1 := Parse(string(data), Limits{})
	fmt.Println("level:", r1.Level, "files:", len(r1.Files), "verify:", len(r1.Manifest.Verify), "problems:", r1.Problems)
}
