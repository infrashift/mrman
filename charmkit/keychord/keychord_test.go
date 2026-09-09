package keychord

import "testing"

// feed runs a key sequence and returns every non-consumed event.
func feed(r *Resolver, keys ...string) []Event {
	var out []Event
	for _, k := range keys {
		if ev := r.Feed(k); !ev.Consumed {
			out = append(out, ev)
		}
	}
	return out
}

func vimResolver() *Resolver {
	return New(Config{Prefixes: []string{"z", "Z", "d"}, Leader: ";", Counts: true})
}

func TestPlainKeysPassStraightThrough(t *testing.T) {
	events := feed(vimResolver(), "j", "k", "q")
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	for i, want := range []string{"j", "k", "q"} {
		if events[i].Key != want || events[i].IsChord() || events[i].Count != 0 {
			t.Errorf("event %d = %+v, want a plain %q", i, events[i], want)
		}
	}
}

func TestChordsCompleteOnTheNextKey(t *testing.T) {
	for _, tc := range []struct {
		keys          []string
		chord, prefix string
		key           string
	}{
		{[]string{"z", "z"}, "zz", "z", "z"},
		{[]string{"z", "t"}, "zt", "z", "t"},
		{[]string{"Z", "Q"}, "ZQ", "Z", "Q"},
		{[]string{"d", "d"}, "dd", "d", "d"},
		{[]string{";", "e"}, ";e", ";", "e"},
	} {
		events := feed(vimResolver(), tc.keys...)
		if len(events) != 1 {
			t.Fatalf("%v: got %d events, want 1", tc.keys, len(events))
		}
		ev := events[0]
		if ev.Chord != tc.chord || ev.Prefix != tc.prefix || ev.Key != tc.key {
			t.Errorf("%v resolved to %+v, want chord %q", tc.keys, ev, tc.chord)
		}
		if !ev.IsChord() {
			t.Errorf("%v must report as a chord", tc.keys)
		}
	}
}

func TestAPrefixHoldsTheKeyBack(t *testing.T) {
	r := vimResolver()
	if ev := r.Feed("z"); !ev.Consumed {
		t.Error("a prefix key must be held back, not dispatched")
	}
	if r.Pending() != "z" {
		t.Errorf("pending = %q, want z", r.Pending())
	}
}

func TestAnUnknownChordIsReportedWholeForTheCallerToJudge(t *testing.T) {
	// keychord does not know which chords exist, so it reports "zq" as a
	// chord and hands the caller both halves. Whether an unrecognized
	// chord falls through to "q" is the caller's policy, not this
	// package's.
	events := feed(vimResolver(), "z", "q")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Chord != "zq" || ev.Prefix != "z" || ev.Key != "q" {
		t.Errorf("got %+v, want chord zq split into prefix z and key q", ev)
	}
}

func TestCountPrefixAccumulates(t *testing.T) {
	events := feed(vimResolver(), "1", "2", "j")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Key != "j" || events[0].Count != 12 {
		t.Errorf("got %+v, want j with count 12", events[0])
	}
}

func TestZeroOnlyExtendsACount(t *testing.T) {
	// A bare "0" is start-of-line, not a count.
	events := feed(vimResolver(), "0")
	if len(events) != 1 || events[0].Key != "0" || events[0].Count != 0 {
		t.Fatalf("bare 0 must dispatch as a key, got %+v", events)
	}

	// After a digit it is part of the number.
	events = feed(vimResolver(), "1", "0", "j")
	if len(events) != 1 || events[0].Count != 10 {
		t.Errorf("10j must count 10, got %+v", events)
	}
}

func TestCountIsCappedAgainstAHeldKey(t *testing.T) {
	r := New(Config{Counts: true, MaxCount: 100})
	for range 20 {
		r.Feed("9")
	}
	ev := r.Feed("j")
	if ev.Count != 100 {
		t.Errorf("count = %d, want the cap 100", ev.Count)
	}
}

func TestCountCombinesWithAChord(t *testing.T) {
	events := feed(vimResolver(), "3", "d", "d")
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Chord != "dd" || events[0].Count != 3 {
		t.Errorf("got %+v, want the chord dd with count 3", events[0])
	}
}

func TestADigitInsideAChordIsTheChordsSecondKey(t *testing.T) {
	// "z5" is a chord completion, not the start of a count.
	events := feed(vimResolver(), "z", "5")
	if len(events) != 1 || events[0].Chord != "z5" {
		t.Errorf("got %+v, want the chord z5", events)
	}
}

func TestCountIsConsumedExactlyOnce(t *testing.T) {
	r := vimResolver()
	first := feed(r, "5", "j")
	second := feed(r, "k")
	if first[0].Count != 5 {
		t.Errorf("first count = %d, want 5", first[0].Count)
	}
	if second[0].Count != 0 {
		t.Errorf("the count must not carry over, got %d", second[0].Count)
	}
}

func TestEffectiveCount(t *testing.T) {
	if got := (Event{Count: 0}).EffectiveCount(1); got != 1 {
		t.Errorf("no count must fall back, got %d", got)
	}
	if got := (Event{Count: 7}).EffectiveCount(1); got != 7 {
		t.Errorf("a typed count must win, got %d", got)
	}
}

func TestResetAbandonsPendingInput(t *testing.T) {
	r := vimResolver()
	r.Feed("3")
	r.Feed("z")
	r.Reset()

	if r.Pending() != "" || r.Count() != 0 {
		t.Error("Reset must clear both the prefix and the count")
	}
	// The next key is plain: the abandoned "z" must not chord with it.
	events := feed(r, "j")
	if len(events) != 1 || events[0].IsChord() || events[0].Count != 0 {
		t.Errorf("got %+v, want a plain j", events)
	}
}

func TestHintShowsInProgressInput(t *testing.T) {
	r := vimResolver()
	if r.Hint() != "" {
		t.Errorf("an idle resolver has no hint, got %q", r.Hint())
	}
	r.Feed("5")
	if r.Hint() != "5" {
		t.Errorf("hint = %q, want 5", r.Hint())
	}
	r.Feed("d")
	if r.Hint() != "5d" {
		t.Errorf("hint = %q, want 5d", r.Hint())
	}
	r.Feed("d")
	if r.Hint() != "" {
		t.Errorf("a completed chord clears the hint, got %q", r.Hint())
	}
}

func TestCountsDisabled(t *testing.T) {
	r := New(Config{Prefixes: []string{"z"}})
	events := feed(r, "5", "j")
	if len(events) != 2 {
		t.Fatalf("without counts a digit is just a key, got %d events", len(events))
	}
	if events[0].Key != "5" {
		t.Errorf("got %+v, want 5 dispatched as a key", events[0])
	}
}

func TestEmptyKeyIsIgnored(t *testing.T) {
	r := vimResolver()
	if ev := r.Feed(""); !ev.Consumed {
		t.Error("an empty key must be swallowed, not dispatched")
	}
}

func TestLeaderIsJustAConfigurablePrefix(t *testing.T) {
	r := New(Config{Leader: ",", Counts: true})
	events := feed(r, ",", "e")
	if len(events) != 1 || events[0].Chord != ",e" {
		t.Errorf("got %+v, want the chord ,e", events)
	}
	// The default leader is not special once rebound.
	events = feed(r, ";")
	if len(events) != 1 || events[0].Key != ";" {
		t.Errorf("an unbound leader must dispatch as a plain key, got %+v", events)
	}
}

func TestEmptyPrefixesAreDropped(t *testing.T) {
	r := New(Config{Prefixes: []string{"", "z"}})
	if ev := r.Feed(""); !ev.Consumed {
		t.Error("an empty string must never become a prefix")
	}
	if r.Pending() != "" {
		t.Error("the empty key must not be pending")
	}
}
