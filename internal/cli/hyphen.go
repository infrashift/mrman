package cli

// preprocessArgs makes pflag tolerate hyphen-leading option values, which
// clap's allow_hyphen_values grants tuicr:
//
//   - `-r -3` / `--revisions -3` is rewritten to `--revisions=-3`
//
// Comment positionals starting with `-` (review add) use the standard `--`
// terminator, which pflag already honors.
func preprocessArgs(argv []string) []string {
	out := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			out = append(out, argv[i:]...)
			break
		}
		if (arg == "-r" || arg == "--revisions") && i+1 < len(argv) {
			next := argv[i+1]
			if len(next) > 0 && next[0] == '-' && next != "--" {
				out = append(out, "--revisions="+next)
				i++
				continue
			}
		}
		out = append(out, arg)
	}
	return out
}
