package reviewcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/infrashift/mrman/internal/textsafe"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/slug"
)

// configUsername is wired to the config package when it lands (M5); until
// then CLI-authored comments fall back to the default author.
var configUsername = func() string { return "" }

// stdinReader is an injection seam for `--input -` tests.
var stdinReader io.Reader = os.Stdin

// Options mirrors the parsed `mrman review` flags.
type Options struct {
	Repo       string
	All        bool
	Session    string
	Input      string
	Type       string
	TargetFile string
	Line       uint32
	EndLine    uint32
	Side       string
	Username   string
	Comment    string
}

// List prints session summaries for `mrman review list`.
func List(store *persistence.Store, opts Options, out io.Writer) error {
	repo := opts.Repo
	if repo == "" {
		repo = "."
	}
	var (
		summaries []persistence.SessionSummary
		err       error
	)
	if opts.All {
		summaries, err = store.ListAllSessions()
	} else {
		summaries, err = store.ListSessions(repo)
	}
	if err != nil {
		return err
	}
	output := make([]SessionSummaryOutput, 0, len(summaries))
	for _, s := range summaries {
		output = append(output, summaryOutput(s))
	}
	return writeJSON(out, output)
}

// Add inserts a comment for `mrman review add` and prints it.
func Add(store *persistence.Store, opts Options, out io.Writer) error {
	path, err := resolveSessionPath(store, opts.Repo, opts.Session)
	if err != nil {
		return err
	}
	target, content, commentType, username, err := buildAddRequest(opts)
	if err != nil {
		return err
	}

	var added *model.Comment
	_, err = store.UpdateSession(path, func(session *model.ReviewSession) error {
		added, err = AddCommentToSession(session, AddCommentRequest{
			Target: target,
			// An agent's comment is input from a process the user does
			// not read before it lands on their screen.
			Content:     textsafe.Sanitize(content),
			CommentType: model.CommentTypeFromID(commentType),
			Author:      resolveAuthor(username),
		})
		return err
	})
	if err != nil {
		return err
	}
	return writeJSON(out, commentOutputFromTarget(target, added))
}

// Comments prints a session's comments for `mrman review comments`.
func Comments(store *persistence.Store, opts Options, out io.Writer) error {
	path, err := resolveSessionPath(store, opts.Repo, opts.Session)
	if err != nil {
		return err
	}
	session, err := store.LoadSession(path)
	if err != nil {
		return err
	}
	return writeJSON(out, collectComments(session))
}

// ResolveSessionPath resolves a slug or path to a session file, exported for
// callers above this package that need the lookup the CLI does.
func ResolveSessionPath(store *persistence.Store, repo, session string) (string, error) {
	return resolveSessionPath(store, repo, session)
}

// resolveSessionPath ports tuicr's resolve_session_ref: direct paths win,
// PR slugs resolve via the manifest, local slugs match within the repo
// selector's listing (erroring on zero or multiple matches).
func resolveSessionPath(store *persistence.Store, repo, session string) (string, error) {
	if repo == "" {
		repo = "."
	}
	if _, statErr := os.Stat(session); statErr == nil ||
		filepath.IsAbs(session) || strings.HasSuffix(session, ".json") {
		return session, nil
	}

	if parsed, parseErr := slug.Parse(session); parseErr == nil {
		if _, isPr := parsed.(slug.PrSlug); isPr {
			all, err := store.ListAllSessions()
			if err != nil {
				return "", err
			}
			for _, s := range all {
				if s.Kind == "pr" && s.Slug == session {
					return s.Path, nil
				}
			}
			return "", &errs.InvalidInput{Detail: fmt.Sprintf(
				"no MR session found for '%s'. Run `mrman review list --all` to see available sessions.", session)}
		}
	}

	summaries, err := store.ListSessions(repo)
	if err != nil {
		return "", err
	}
	var matches []persistence.SessionSummary
	for _, s := range summaries {
		if s.Slug == session {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0].Path, nil
	case 0:
		return "", &errs.InvalidInput{Detail: fmt.Sprintf(
			"session '%s' was not found for repo %s. Run `mrman review list --repo %s` to see available sessions.",
			session, repo, repo)}
	default:
		return "", &errs.InvalidInput{Detail: fmt.Sprintf(
			"session '%s' is ambiguous for repo %s", session, repo)}
	}
}

// addPayload is the machine JSON accepted by --input.
type addPayload struct {
	CommentType *string        `json:"comment_type"`
	Type        *string        `json:"type"`
	Content     *string        `json:"content"`
	Target      *targetPayload `json:"target"`
	File        *string        `json:"file"`
	Line        *uint32        `json:"line"`
	StartLine   *uint32        `json:"start_line"`
	EndLine     *uint32        `json:"end_line"`
	Side        *string        `json:"side"`
	Username    *string        `json:"username"`
	Author      *string        `json:"author"`
}

type targetPayload struct {
	TargetType *string `json:"type"`
	Kind       *string `json:"kind"`
	File       *string `json:"file"`
	Line       *uint32 `json:"line"`
	StartLine  *uint32 `json:"start_line"`
	EndLine    *uint32 `json:"end_line"`
	Side       *string `json:"side"`
}

// addInputs is everything `review add` needs, gathered from the flags and
// then overridden by the JSON payload, field by field.
type addInputs struct {
	file          string
	line, endLine *uint32
	side          string
	content       string
	commentType   string
	username      string
	// explicit is a target the payload spelled out in full, which wins
	// over the file/line fields.
	explicit *CommentTarget
}

func addInputsFromFlags(opts Options) addInputs {
	in := addInputs{
		file: opts.TargetFile, side: opts.Side, content: opts.Comment,
		commentType: opts.Type, username: opts.Username,
	}
	if in.commentType == "" {
		in.commentType = "none"
	}
	if in.side == "" {
		in.side = "new"
	}
	if opts.Line != 0 {
		l := opts.Line
		in.line = &l
	}
	if opts.EndLine != 0 {
		l := opts.EndLine
		in.endLine = &l
	}
	return in
}

// applyPayload overlays the JSON payload. Both spellings of each aliased
// field are accepted (type/comment_type, username/author, target/file+line).
func (in *addInputs) applyPayload(payload *addPayload) error {
	if payload.CommentType != nil {
		in.commentType = *payload.CommentType
	} else if payload.Type != nil {
		in.commentType = *payload.Type
	}
	if payload.Content != nil {
		in.content = *payload.Content
	}
	if payload.Username != nil {
		in.username = *payload.Username
	} else if payload.Author != nil {
		in.username = *payload.Author
	}
	if payload.Target != nil {
		t, err := payload.Target.toCommentTarget()
		if err != nil {
			return err
		}
		in.explicit = &t
		return nil
	}
	if payload.File != nil {
		in.file = *payload.File
	}
	if payload.Line != nil {
		in.line = payload.Line
	} else if payload.StartLine != nil {
		in.line = payload.StartLine
	}
	if payload.EndLine != nil {
		in.endLine = payload.EndLine
	}
	if payload.Side != nil {
		in.side = *payload.Side
	}
	return nil
}

// readAddPayload reads and decodes the --input payload.
func readAddPayload(input string) (addPayload, error) {
	raw, err := readJSONInput(input)
	if err != nil {
		return addPayload{}, err
	}
	var payload addPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return addPayload{}, &errs.InvalidInput{Detail: "invalid JSON review payload: " + err.Error()}
	}
	return payload, nil
}

func buildAddRequest(opts Options) (CommentTarget, string, string, string, error) {
	in := addInputsFromFlags(opts)
	if opts.Input != "" {
		payload, err := readAddPayload(opts.Input)
		if err != nil {
			return CommentTarget{}, "", "", "", err
		}
		if err := in.applyPayload(&payload); err != nil {
			return CommentTarget{}, "", "", "", err
		}
	}
	if strings.TrimSpace(in.content) == "" {
		return CommentTarget{}, "", "", "", &errs.InvalidInput{
			Detail: "comment text is required either as COMMENT or JSON field `content`"}
	}
	if in.explicit != nil {
		return *in.explicit, in.content, in.commentType, in.username, nil
	}
	target, err := buildCommentTarget(in.file, in.line, in.endLine, in.side)
	return target, in.content, in.commentType, in.username, err
}

func readJSONInput(input string) (string, error) {
	if input == "-" {
		data, err := io.ReadAll(stdinReader)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	if path, ok := strings.CutPrefix(input, "@"); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return input, nil
}

func (p *targetPayload) toCommentTarget() (CommentTarget, error) {
	side := model.LineSideNew
	if p.Side != nil {
		parsed, err := parseLineSide(*p.Side)
		if err != nil {
			return CommentTarget{}, err
		}
		side = parsed
	}

	inferred := "file"
	switch {
	case p.File == nil:
		inferred = "review"
	case p.Line != nil || p.StartLine != nil:
		if p.EndLine != nil {
			inferred = "line_range"
		} else {
			inferred = "line"
		}
	}
	targetType := inferred
	if p.TargetType != nil {
		targetType = *p.TargetType
	} else if p.Kind != nil {
		targetType = *p.Kind
	}
	targetType = strings.ToLower(strings.ReplaceAll(targetType, "-", "_"))

	lineOrStart := p.Line
	if lineOrStart == nil {
		lineOrStart = p.StartLine
	}

	switch targetType {
	case "review":
		return CommentTarget{Kind: TargetReview}, nil
	case "file":
		path, err := requiredField(p.File, "target.file")
		if err != nil {
			return CommentTarget{}, err
		}
		return CommentTarget{Kind: TargetFile, Path: path}, nil
	case "line":
		path, err := requiredField(p.File, "target.file")
		if err != nil {
			return CommentTarget{}, err
		}
		line, err := requiredLine(lineOrStart, "target.line")
		if err != nil {
			return CommentTarget{}, err
		}
		return CommentTarget{Kind: TargetLine, Path: path, Line: line, Side: side}, nil
	case "line_range", "range":
		path, err := requiredField(p.File, "target.file")
		if err != nil {
			return CommentTarget{}, err
		}
		start, err := requiredLine(lineOrStart, "target.start_line")
		if err != nil {
			return CommentTarget{}, err
		}
		end, err := requiredLine(p.EndLine, "target.end_line")
		if err != nil {
			return CommentTarget{}, err
		}
		return CommentTarget{
			Kind: TargetLineRange, Path: path,
			Range: model.NewLineRange(start, end), Side: side,
		}, nil
	}
	return CommentTarget{}, &errs.InvalidInput{Detail: fmt.Sprintf("unknown JSON target type '%s'", targetType)}
}

func buildCommentTarget(file string, line, endLine *uint32, sideArg string) (CommentTarget, error) {
	side, err := parseLineSide(sideArg)
	if err != nil {
		return CommentTarget{}, err
	}
	switch {
	case file == "" && line == nil && endLine == nil:
		return CommentTarget{Kind: TargetReview}, nil
	case file != "" && line == nil && endLine == nil:
		return CommentTarget{Kind: TargetFile, Path: file}, nil
	case file != "" && line != nil && endLine == nil:
		if err := validateLine(*line, "--line"); err != nil {
			return CommentTarget{}, err
		}
		return CommentTarget{Kind: TargetLine, Path: file, Line: *line, Side: side}, nil
	case file != "" && line != nil && endLine != nil:
		if err := validateLine(*line, "--line"); err != nil {
			return CommentTarget{}, err
		}
		if err := validateLine(*endLine, "--end-line"); err != nil {
			return CommentTarget{}, err
		}
		return CommentTarget{
			Kind: TargetLineRange, Path: file,
			Range: model.NewLineRange(*line, *endLine), Side: side,
		}, nil
	case file == "" && line != nil:
		return CommentTarget{}, &errs.InvalidInput{Detail: "--line requires --target-file for review comments"}
	case file == "" && endLine != nil:
		return CommentTarget{}, &errs.InvalidInput{Detail: "--end-line requires --line and --target-file"}
	default:
		return CommentTarget{}, &errs.InvalidInput{Detail: "--end-line requires --line"}
	}
}

func parseLineSide(s string) (model.LineSide, error) {
	switch strings.ToLower(s) {
	case "old":
		return model.LineSideOld, nil
	case "new":
		return model.LineSideNew, nil
	}
	return "", &errs.InvalidInput{Detail: fmt.Sprintf("unknown side '%s', expected 'old' or 'new'", s)}
}

func requiredField(v *string, name string) (string, error) {
	if v == nil || *v == "" {
		return "", &errs.InvalidInput{Detail: name + " is required"}
	}
	return *v, nil
}

func requiredLine(v *uint32, name string) (uint32, error) {
	if v == nil {
		return 0, &errs.InvalidInput{Detail: name + " is required"}
	}
	if err := validateLine(*v, name); err != nil {
		return 0, err
	}
	return *v, nil
}

func validateLine(line uint32, name string) error {
	if line == 0 {
		return &errs.InvalidInput{Detail: name + " must be greater than zero"}
	}
	return nil
}

func resolveAuthor(explicit string) string {
	if name := strings.TrimSpace(explicit); name != "" {
		return name
	}
	if name := strings.TrimSpace(configUsername()); name != "" {
		return name
	}
	return model.DefaultAuthor
}

// writeJSON pretty-prints v without HTML escaping and with a trailing
// newline, matching serde_json::to_writer_pretty + writeln.
func writeJSON(out io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := out.Write(buf.Bytes())
	return err
}
