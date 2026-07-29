package ui

import "fmt"

// helpContent returns the help popup's text rows, ported from tuicr's help
// sections (read-only subset; review/comment rows land with M4).
func helpContent(leader rune) []string {
	l := string(leader)
	return []string{
		" Help (j/k scroll, / search) — press ? or Esc to close",
		"",
		" Navigation",
		"  j/k         move cursor down/up",
		"  Ctrl-d/u    half page down/up",
		"  Ctrl-f/b    full page down/up",
		"  Ctrl-e/y    scroll view without moving cursor",
		"  g/G         top / bottom ({N}G jumps to source line N)",
		"  zz/zt/zb    center / top / bottom cursor",
		"  }/{         next/previous file",
		"  ]/[         next/previous hunk",
		"  h/l         scroll horizontally",
		"  Space       expand/collapse context gap",
		"",
		" Panels",
		"  Tab         switch focus between file list and diff",
		fmt.Sprintf("  %se          toggle the file list", l),
		fmt.Sprintf("  %sh/%sl       focus file list / diff", l, l),
		fmt.Sprintf("  %sf          toggle single-file view", l),
		"",
		" File tree",
		"  Enter       open file / toggle directory",
		"  o/O         expand / collapse all directories",
		"",
		" Search",
		"  /           search",
		"  n/N         next / previous match",
		"",
		" Commands",
		"  :q          quit    :wrap  toggle wrap    :diff  unified/side-by-side",
		"  :{N}        jump to line N (new side)     :o{N}  old side",
		"",
		" Quit",
		"  q           quit    ZZ/ZQ  quit           Ctrl-C twice  force quit",
	}
}
