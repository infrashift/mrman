package diffparser

import (
	"fmt"
	"strings"
	"testing"
)

// benchDiff is a git diff of files files, each with hunks hunks of a
// context/deletion/addition mix lines long.
func benchDiff(files, hunks, lines int) string {
	var b strings.Builder
	for f := range files {
		path := fmt.Sprintf("pkg/file%03d.go", f)
		fmt.Fprintf(&b, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", path, path, path, path)
		for h := range hunks {
			start := 1 + h*(lines+20)
			ctx, del, add := lines-lines/5*2, lines/5, lines/5
			fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", start, ctx+del, start, ctx+add)
			for i := range ctx {
				fmt.Fprintf(&b, " context line %d\n", i)
			}
			for i := range del {
				fmt.Fprintf(&b, "-removed line %d\n", i)
			}
			for i := range add {
				fmt.Fprintf(&b, "+added line %d\n", i)
			}
		}
	}
	return b.String()
}

func BenchmarkParse50kLines(b *testing.B) {
	text := benchDiff(200, 5, 50)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := Parse(text, GitStyle, nil); err != nil {
			b.Fatal(err)
		}
	}
}
