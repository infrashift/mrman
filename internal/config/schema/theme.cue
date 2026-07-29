// Schema for mrman local theme TOML files. Every palette key is required;
// values are #RRGGBB hex or named terminal colors. The optional syntax keys
// select a chroma style (mrman does not support .tmTheme files).
package schema

// #ThemeNamedColor enumerates the named terminal colors accepted anywhere a
// palette value appears (mirrors tuicr's list).
#ThemeNamedColor: "black" | "red" | "green" | "yellow" | "blue" | "magenta" |
	"cyan" | "gray" | "grey" | "darkgray" | "dark_gray" | "darkgrey" |
	"dark_grey" | "lightred" | "light_red" | "lightgreen" | "light_green" |
	"lightyellow" | "light_yellow" | "lightblue" | "light_blue" |
	"lightmagenta" | "light_magenta" | "lightcyan" | "light_cyan" | "white"

// #ThemeColor is a palette value: "#RRGGBB" hex or a named terminal color.
#ThemeColor: =~"^#[0-9a-fA-F]{6}$" | #ThemeNamedColor

// #Theme validates a local theme file.
#Theme: {
	// Base colors
	panel_bg!:     #ThemeColor
	bg_highlight!: #ThemeColor
	fg_primary!:   #ThemeColor
	fg_secondary!: #ThemeColor
	fg_dim!:       #ThemeColor

	// Diff colors
	diff_add!:            #ThemeColor
	diff_add_bg!:         #ThemeColor
	diff_del!:            #ThemeColor
	diff_del_bg!:         #ThemeColor
	diff_context!:        #ThemeColor
	diff_hunk_header!:    #ThemeColor
	expanded_context_fg!: #ThemeColor

	// Syntax-highlighted diff backgrounds
	syntax_add_bg!: #ThemeColor
	syntax_del_bg!: #ThemeColor

	// File status colors
	file_added!:    #ThemeColor
	file_modified!: #ThemeColor
	file_deleted!:  #ThemeColor
	file_renamed!:  #ThemeColor

	// Review status colors
	reviewed!: #ThemeColor
	pending!:  #ThemeColor

	// Comment type colors
	comment_note!:       #ThemeColor
	comment_suggestion!: #ThemeColor
	comment_issue!:      #ThemeColor
	comment_praise!:     #ThemeColor

	// UI element colors
	border_focused!:   #ThemeColor
	border_unfocused!: #ThemeColor
	status_bar_bg!:    #ThemeColor
	cursor_color!:     #ThemeColor
	cursor_line_bg!:   #ThemeColor
	branch_name!:      #ThemeColor
	help_indicator!:   #ThemeColor

	// Message / badge colors
	message_info_fg!:    #ThemeColor
	message_info_bg!:    #ThemeColor
	message_warning_fg!: #ThemeColor
	message_warning_bg!: #ThemeColor
	message_error_fg!:   #ThemeColor
	message_error_bg!:   #ThemeColor
	update_badge_fg!:    #ThemeColor
	update_badge_bg!:    #ThemeColor

	// Mode indicator colors
	mode_fg!: #ThemeColor
	mode_bg!: #ThemeColor

	// Optional chroma syntax style: a registered style name, or a path to a
	// chroma XML style file (resolved relative to the theme file).
	syntax_style?:      string
	syntax_style_file?: string
}
