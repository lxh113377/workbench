package artifact

import (
	"fmt"
	"strings"
	"testing"
)

func TestDebugStrict(t *testing.T) {
	f3 := strings.Repeat(testFence, 3)
	raw := "<<<ARTIFACTS>>>\n" + f3 + "manifest\n" +
		"{\"files\":[{\"path\":\"a.md\"}],\"verify\":[{\"cmd\":\"pwsh -Command exit 0\",\"expectExit\":0}]}\n" +
		f3 + "\n" + f3 + "file:a.md\n# hi\n" + f3 + "\n<<<END>>>\n"
	body, _ := extractBlock(raw)
	st := &Result{Raw: raw, Level: LevelStrict}
	parseStrict(body, st)
	fmt.Println("strict files:", len(st.Files), "problems:", st.Problems, "verify:", len(st.Manifest.Verify))
	r := Parse(raw, Limits{})
	fmt.Println("level:", r.Level, "files:", len(r.Files), "verify:", len(r.Manifest.Verify), "problems:", r.Problems)
}
