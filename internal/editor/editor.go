// Package editor builds $EDITOR invocations for jumping from a review to a
// source line, ported from tuicr's editor module. The editor string is split
// shell-style but executed directly, never through a shell.
package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	shellquote "github.com/kballard/go-shellquote"
)

// Target is a file (absolute path) and optional 1-based line to open.
type Target struct {
	Path string
	Line *uint32
}

// family classifies how an editor accepts a line number.
type family int

const (
	familyPlain    family = iota
	familyPlusLine        // vi/vim/nvim/nano: editor +42 path
	familyGotoLine        // VS Code family: editor --goto path:42
)

func familyFor(program string) family {
	switch filepath.Base(program) {
	case "vi", "vim", "nvim", "nano":
		return familyPlusLine
	case "code", "code-insiders", "codium", "cursor":
		return familyGotoLine
	}
	return familyPlain
}

// Command is a ready-to-run editor invocation.
type Command struct {
	Program string
	Args    []string
}

// FromEnv builds the command from $EDITOR, falling back to vi when unset,
// empty, or unparsable.
func FromEnv(target Target) Command {
	return FromEditor(os.Getenv("EDITOR"), target)
}

// FromEditor builds the command from an editor string (shell-style quoting
// honored; user-supplied args preserved).
func FromEditor(editorStr string, target Target) Command {
	words, err := shellquote.Split(editorStr)
	if err != nil || len(words) == 0 {
		words = []string{"vi"}
	}
	program, userArgs := words[0], words[1:]

	args := append([]string(nil), userArgs...)
	switch f := familyFor(program); {
	case f == familyPlusLine && target.Line != nil:
		args = append(args, "+"+strconv.FormatUint(uint64(*target.Line), 10), target.Path)
	case f == familyGotoLine && target.Line != nil:
		args = append(args, "--goto", target.Path+":"+strconv.FormatUint(uint64(*target.Line), 10))
	default:
		args = append(args, target.Path)
	}
	return Command{Program: program, Args: args}
}

// ExecCommand returns the exec.Cmd for tea.ExecProcess wiring.
func (c Command) ExecCommand() *exec.Cmd {
	return exec.Command(c.Program, c.Args...)
}
