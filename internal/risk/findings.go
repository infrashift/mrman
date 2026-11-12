package risk

import vmarker "github.com/infrashift/vizzini-pr-reviewer/pkg/marker"

// The format of vizzini-bot's comments (Vizzini ISSUE-027), for the
// findings view: pkg/marker is standard-library only and LLM-free, like
// pkg/risk, so it may sit behind this package too.

// FindingComment is a finding comment as vizzini-bot posts it.
type FindingComment = vmarker.Comment

// FindingMarker is the hidden marker a finding comment carries.
type FindingMarker = vmarker.Marker

// ParseFindingComment decodes a finding comment; ok is false when body
// carries no finding marker.
func ParseFindingComment(body string) (FindingComment, bool) { return vmarker.ParseComment(body) }

// FromVizzini reports whether vizzini-bot wrote body.
func FromVizzini(body string) bool { return vmarker.FromVizzini(body) }

// ReplyTo is the comment id a vizzini-bot answer answers ("" if none).
func ReplyTo(body string) string { return vmarker.ReplyTo(body) }
