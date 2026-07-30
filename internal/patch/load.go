package patch

import (
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
)

// Options tunes how an artifact is read.
type Options struct {
	// StripLevel is how many leading path components to drop from the paths
	// the patch declares. Zero means DefaultStripLevel; use -1 for none.
	StripLevel int
}

// stripLevel resolves the option to a concrete count.
func (o Options) stripLevel() int {
	switch {
	case o.StripLevel < 0:
		return 0
	case o.StripLevel == 0:
		return DefaultStripLevel
	}
	return o.StripLevel
}

// LoadFile reads and parses a patch artifact from disk.
func LoadFile(path string, opts Options) (*Series, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	series, err := Load(string(data), opts)
	if err != nil {
		return nil, err
	}
	series.Source = path
	return series, nil
}

// LoadReader reads and parses a patch artifact from r.
func LoadReader(r io.Reader, opts Options) (*Series, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return Load(string(data), opts)
}

// Load parses a patch artifact: a bare diff, a mail message, or an mbox.
//
// It returns *errs.InvalidInput when the text is not a patch at all, which is
// a better answer than diffparser's ErrNoChanges — "no changes to review"
// sends someone hunting for an empty diff when the real problem is that they
// pointed mrman at a text file.
func Load(text string, opts Options) (*Series, error) {
	text = normalizeNewlines(text)
	kind := Detect(text)
	strip := opts.stripLevel()

	series := &Series{
		Kind:        kind,
		ContentHash: contentHash(text),
	}

	switch kind {
	case KindMbox:
		for _, msg := range splitMbox(text) {
			series.Patches = append(series.Patches, patchFromMessage(parseMessage(msg), strip))
		}
	case KindMail:
		series.Patches = append(series.Patches, patchFromMessage(parseMessage(text), strip))
	case KindDiff:
		series.Patches = append(series.Patches, Patch{
			Version:  1,
			DiffText: Normalize(text, strip),
		})
	default:
		return nil, &errs.InvalidInput{
			Detail: "not a patch, diff, or mbox",
		}
	}

	if !series.hasReviewableContent() {
		return nil, &errs.InvalidInput{Detail: fmt.Sprintf(
			"%s carries no diff — nothing to review", kind)}
	}
	return series, nil
}

// hasReviewableContent reports whether any patch carries a diff. A cover
// letter alone is prose, not a review target.
func (s *Series) hasReviewableContent() bool {
	for i := range s.Patches {
		if strings.TrimSpace(s.Patches[i].DiffText) != "" {
			return true
		}
	}
	return false
}

// patchFromMessage builds a Patch from one parsed message.
func patchFromMessage(msg message, strip int) Patch {
	rawSubject := header(msg.headers, "Subject")
	summary, version, pos, total := parseSubject(rawSubject)
	changelog, diff := splitBody(msg.body)

	return Patch{
		MessageID:  trimAngles(header(msg.headers, "Message-Id")),
		InReplyTo:  trimAngles(header(msg.headers, "In-Reply-To")),
		References: splitRefs(header(msg.headers, "References")),
		Subject:    summary,
		RawSubject: decodeWords(rawSubject),
		Version:    version,
		SeriesPos:  pos,
		SeriesLen:  total,
		Author:     decodeWords(header(msg.headers, "From")),
		Date:       parseDate(header(msg.headers, "Date")),
		Changelog:  changelog,
		DiffText:   Normalize(diff, strip),
	}
}

// contentHash is the artifact's identity: the bytes as read, before any
// normalisation, so that an edited or re-sent patch is a different review.
func contentHash(text string) uint64 {
	h := fnv.New64a()
	_, _ = io.WriteString(h, text)
	return h.Sum64()
}
