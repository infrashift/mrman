# charmkit

Three layers a vim-flavored terminal UI needs and the Charm libraries do not
provide. Extracted from [mrman](https://github.com/infrashift/mrman), where
each is load-bearing, so they are shaped by a real application rather than
by guesses about one.

```
go get github.com/infrashift/mrman/charmkit
```

`keychord` and `cellrender` depend on nothing but `go-runewidth`, so they
compose with whatever you already use. `vimtext` takes Bubble Tea v2's
`tea.Key`, the key event a Charm application already has, so it imports
`charm.land/bubbletea/v2`. Nothing here imports lipgloss.

## `vimtext` — modal text editing

`bubbles/textarea` has no modal editing at all. This is a text buffer that
speaks vim:

- Modes: Normal, Insert, Visual, Visual-Line
- Motions `h j k l w b e 0 ^ $ gg G` with counts
- Insert entry `i a I A o O`
- Edits `x s dd D C cc d{motion} c{motion} y{motion} ciw diw`
- Registers with linewise-aware `yy p P`
- Undo and redo (`u`, `Ctrl-r`) with insert-run coalescing, so undo steps
  match what a typist expects rather than one character at a time

```go
ed := vimtext.New("hello world", 0) // starts in Insert mode
ed.TabWidth = 4
ed.EnterNormal()
ed.HandleKey(tea.Key{Code: 'd', Text: "d"})
ed.HandleKey(tea.Key{Code: 'w', Text: "w"})
fmt.Println(ed.Text()) // "world"
```

`HandleKey` reports whether it consumed the key; the ones it hands back
(Enter or Esc in Normal mode with nothing pending, `:`, Tab, Ctrl-S,
Alt-modified keys) are yours to bind. This example runs as `Example` in
`vimtext/example_test.go`.

It owns the buffer and the editing model, not the rendering: you draw
`Text()` and place your cursor at `Cursor()`, styled however you like.

## `keychord` — chords and count prefixes

`bubbles/key` matches one key at a time. The layer above it — knowing that
`5` is not a command, that `z` alone is not either but `zz` is, and that the
`3` in `3}` belongs to the `}` — is what every vim-flavored TUI ends up
writing by hand.

```go
r := keychord.New(keychord.Config{
    Prefixes: []string{"z", "Z", "d"},
    Leader:   ";",
    Counts:   true,
})

ev := r.Feed("3")   // ev.Consumed
ev = r.Feed("d")    // ev.Consumed — a prefix, held back
ev = r.Feed("d")    // ev.Chord == "dd", ev.Count == 3
```

Keys are plain strings, so it is independent of any event type: convert your
framework's key to a string and feed it in. Only declared prefixes and
count digits are ever held back; everything else passes straight through.

`keychord` does not know which chords you have bound. An unrecognized
combination is reported whole — `Chord`, `Prefix` and `Key` — and you decide
whether it does nothing or whether its second key should still act. That
policy belongs to your keymap, not here.

`Hint()` renders in-progress input (`5`, `z`, `3d`) for a status line.
`Reset()` abandons a half-typed chord when input context changes, which you
want on every mode switch: otherwise a stray prefix completes against a text
field.

## `cellrender` — a terminal-cell text layer

lipgloss styles blocks, not cell streams. It gives you no horizontal scroll,
and no way to repaint a row's background such that the paint stays aligned
under word wrap. Anything that scrolls a wide document sideways — a diff, a
log, a wide table — needs a layer underneath it.

- `Span` / `Style`: styled runs, `nil` colors meaning "inherit"
- `LogicalLine` with a caller-defined `RowKind`, so a row's meaning survives
  wrapping, truncation and re-styling — no zero-width marker runes hidden in
  the text
- Display-width-accurate `WrapSpans`, `TruncateStr`, `TruncateOrPadSpans`
- `ApplyHorizontalScroll`, which never splits a wide rune in half
- `OverrideBg`, `FillBg`, `PadToWidth` for cursor lines and selections
- `Emitter`, which serializes spans to ANSI once per frame

```go
e := &cellrender.Emitter{}
line := e.Line([]cellrender.Span{
    {Text: "func ", Style: cellrender.Style{Fg: keyword, Bold: true}},
    {Text: "main()", Style: cellrender.Style{Fg: ident}},
})
```

Width math is pinned: `EastAsianWidth` is forced off so layout is
deterministic regardless of the user's locale.

## Stability

These are extracted from a working application and covered at 95%+, but the
module has not settled into a v1. Expect the occasional breaking change
until it does, and pin a version.

## License

Same as mrman.
