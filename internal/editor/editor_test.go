package editor

import (
	"strings"
	"testing"
)

func line(n uint32) *uint32 { return &n }

func TestPlusLineFamily(t *testing.T) {
	for _, ed := range []string{"vi", "vim", "nvim", "nano"} {
		cmd := FromEditor(ed, Target{Path: "/repo/src/main.go", Line: line(42)})
		if cmd.Program != ed || len(cmd.Args) != 2 ||
			cmd.Args[0] != "+42" || cmd.Args[1] != "/repo/src/main.go" {
			t.Errorf("%s: got %+v", ed, cmd)
		}
	}
}

func TestGotoLineFamily(t *testing.T) {
	for _, ed := range []string{"code", "code-insiders", "codium", "cursor"} {
		cmd := FromEditor(ed, Target{Path: "/repo/a.go", Line: line(7)})
		if len(cmd.Args) != 2 || cmd.Args[0] != "--goto" || cmd.Args[1] != "/repo/a.go:7" {
			t.Errorf("%s: got %+v", ed, cmd)
		}
	}
}

func TestPlainFamilyAndNoLine(t *testing.T) {
	cmd := FromEditor("emacs", Target{Path: "/x.go", Line: line(3)})
	if len(cmd.Args) != 1 || cmd.Args[0] != "/x.go" {
		t.Errorf("plain: %+v", cmd)
	}
	// Line-aware families without a line just get the path.
	cmd = FromEditor("vim", Target{Path: "/x.go"})
	if len(cmd.Args) != 1 || cmd.Args[0] != "/x.go" {
		t.Errorf("no line: %+v", cmd)
	}
}

func TestUserArgsPreservedAndQuoting(t *testing.T) {
	cmd := FromEditor(`vim -u "/home/me/my vimrc"`, Target{Path: "/x.go", Line: line(9)})
	if cmd.Program != "vim" || len(cmd.Args) != 4 {
		t.Fatalf("got %+v", cmd)
	}
	if cmd.Args[0] != "-u" || cmd.Args[1] != "/home/me/my vimrc" ||
		cmd.Args[2] != "+9" || cmd.Args[3] != "/x.go" {
		t.Fatalf("got %+v", cmd.Args)
	}
}

func TestFallbackToVi(t *testing.T) {
	for _, bad := range []string{"", `unclosed "quote`} {
		cmd := FromEditor(bad, Target{Path: "/x.go", Line: line(2)})
		if cmd.Program != "vi" || cmd.Args[0] != "+2" {
			t.Errorf("%q: got %+v", bad, cmd)
		}
	}
}

func TestFamilyByBasename(t *testing.T) {
	cmd := FromEditor("/usr/local/bin/nvim", Target{Path: "/x.go", Line: line(5)})
	if cmd.Args[0] != "+5" {
		t.Errorf("path-qualified nvim must be PlusLine: %+v", cmd)
	}
}

func TestExecCommand(t *testing.T) {
	cmd := FromEditor("vim", Target{Path: "/x.go"}).ExecCommand()
	if !strings.HasSuffix(cmd.Path, "vim") && cmd.Args[0] != "vim" {
		t.Errorf("exec cmd = %+v", cmd.Args)
	}
}
