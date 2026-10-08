package vimtext_test

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/charmkit/vimtext"
)

// The README's example, kept here so it cannot drift from the API: an editor
// starts in Insert mode, so leave it before typing a Normal-mode command.
func Example() {
	ed := vimtext.New("hello world", 0)
	ed.TabWidth = 4
	ed.EnterNormal()
	ed.HandleKey(tea.Key{Code: 'd', Text: "d"})
	ed.HandleKey(tea.Key{Code: 'w', Text: "w"})
	fmt.Println(ed.Text())
	// Output: world
}
