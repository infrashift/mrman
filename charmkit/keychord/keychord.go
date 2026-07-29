// Package keychord resolves vim-style key input: count prefixes like "5j",
// two-key chords like "zz" and "dd", and leader chords like ";e".
//
// It exists because the usual key-binding helpers match one key at a time.
// Anything vim-flavored needs the layer above that — the part that knows
// "5" is not a command, that "z" alone is not either but "zz" is, and that
// the "3" in "3}" belongs to the "}" that follows. Every such TUI ends up
// writing this state machine by hand; this is that state machine, extracted.
//
// Keys are plain strings so the package stays independent of any particular
// event type: convert your framework's key event to a string ("j", "Z",
// "enter") and feed it in. Only the keys you declare as prefixes, and the
// digits when counts are enabled, are ever held back.
//
// A Resolver is not safe for concurrent use; drive it from one event loop.
package keychord

import "strings"

// DefaultMaxCount caps an accumulating count prefix. Without a cap a user
// leaning on a digit key can drive the count — and whatever it multiplies —
// to absurd values.
const DefaultMaxCount = 999_999

// Config declares what a Resolver should treat as special.
type Config struct {
	// Prefixes are keys that begin a chord: "z", "Z", "d", and often a
	// leader. A prefix key is held back until the next key completes it.
	Prefixes []string
	// Leader, when non-empty, is added to Prefixes. It is named separately
	// because it is the one users rebind.
	Leader string
	// Counts enables digit-prefix accumulation. "0" only starts a count
	// when one is already in progress, so "0" keeps its usual
	// start-of-line meaning.
	Counts bool
	// MaxCount caps the count; zero means DefaultMaxCount.
	MaxCount int
}

// Resolver turns a key stream into Events.
type Resolver struct {
	prefixes map[string]bool
	counts   bool
	maxCount int

	pending string
	count   int
}

// New returns a Resolver for the given configuration.
func New(cfg Config) *Resolver {
	prefixes := make(map[string]bool, len(cfg.Prefixes)+1)
	for _, p := range cfg.Prefixes {
		if p != "" {
			prefixes[p] = true
		}
	}
	if cfg.Leader != "" {
		prefixes[cfg.Leader] = true
	}
	maxCount := cfg.MaxCount
	if maxCount <= 0 {
		maxCount = DefaultMaxCount
	}
	return &Resolver{prefixes: prefixes, counts: cfg.Counts, maxCount: maxCount}
}

// Event is the outcome of feeding one key.
type Event struct {
	// Consumed means the key was absorbed as a prefix or a count digit and
	// there is nothing to dispatch yet.
	Consumed bool
	// Chord is the completed two-key chord ("zz", ";e"), empty for a plain
	// key. Prefix is its first key and Key its second.
	Chord  string
	Prefix string
	// Key is the key to act on: the plain key, or a chord's second key.
	Key string
	// Count is the accumulated count prefix, 0 when none was typed.
	Count int
}

// IsChord reports whether the event completed a chord.
func (e Event) IsChord() bool { return e.Chord != "" }

// EffectiveCount returns Count, or fallback when no count was typed. Use it
// for motions that repeat: `count := ev.EffectiveCount(1)`.
func (e Event) EffectiveCount(fallback int) int {
	if e.Count == 0 {
		return fallback
	}
	return e.Count
}

// Feed advances the resolver by one key.
//
// A pending prefix always resolves on the very next key, whatever it is:
// "zq" completes as the chord "zq" rather than silently dropping the "z"
// and running "q". Callers decide that an unknown chord does nothing, which
// keeps a mistyped chord from firing an unrelated command.
func (r *Resolver) Feed(key string) Event {
	if key == "" {
		return Event{Consumed: true}
	}

	// Completing a pending chord takes priority over everything: inside a
	// chord even a digit is just the chord's second key.
	if r.pending != "" {
		prefix := r.pending
		r.pending = ""
		count := r.takeCount()
		return Event{Chord: prefix + key, Prefix: prefix, Key: key, Count: count}
	}

	if r.counts && isCountDigit(key, r.count > 0) {
		r.count = min(r.count*10+int(key[0]-'0'), r.maxCount)
		return Event{Consumed: true}
	}

	if r.prefixes[key] {
		r.pending = key
		return Event{Consumed: true}
	}

	return Event{Key: key, Count: r.takeCount()}
}

// isCountDigit reports whether key starts or extends a count. "0" extends
// one but never starts it.
func isCountDigit(key string, inProgress bool) bool {
	if len(key) != 1 || key[0] < '0' || key[0] > '9' {
		return false
	}
	return key[0] != '0' || inProgress
}

func (r *Resolver) takeCount() int {
	count := r.count
	r.count = 0
	return count
}

// Reset abandons any pending prefix and count. Call it when input context
// changes underneath the resolver — a mode switch, or a focus change — so a
// half-typed chord cannot complete against the wrong handler.
func (r *Resolver) Reset() {
	r.pending = ""
	r.count = 0
}

// Pending returns the prefix awaiting completion, "" when none is.
func (r *Resolver) Pending() string { return r.pending }

// Count returns the count accumulated so far, 0 when none is.
func (r *Resolver) Count() int { return r.count }

// Hint renders the in-progress input for a status line: "5", "z", "3d".
// Empty when the resolver is idle, so a caller can show it unconditionally.
func (r *Resolver) Hint() string {
	var b strings.Builder
	if r.count > 0 {
		b.WriteString(itoa(r.count))
	}
	b.WriteString(r.pending)
	return b.String()
}

// itoa avoids pulling strconv in for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
