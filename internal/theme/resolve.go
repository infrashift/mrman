package theme

import (
	"errors"
	"fmt"
	"strings"
)

// Appearance selects between the dark and light default themes, or defers
// to system detection. The zero value AppearanceUnset means "not
// configured" (tuicr's Option<AppearanceArg> = None).
type Appearance int

// Appearance values.
const (
	AppearanceUnset Appearance = iota
	AppearanceDark
	AppearanceLight
	AppearanceSystem
)

// String returns the canonical configuration spelling of the appearance.
func (a Appearance) String() string {
	switch a {
	case AppearanceDark:
		return "dark"
	case AppearanceLight:
		return "light"
	case AppearanceSystem:
		return "system"
	default:
		return "unset"
	}
}

// ParseAppearance parses "dark", "light", or "system" (case-insensitive,
// surrounding whitespace ignored) into an Appearance.
func ParseAppearance(s string) (Appearance, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dark":
		return AppearanceDark, nil
	case "light":
		return AppearanceLight, nil
	case "system":
		return AppearanceSystem, nil
	default:
		return AppearanceUnset, fmt.Errorf("unknown appearance %q, valid options: light, dark, system", s)
	}
}

// Resolve implements tuicr's theme-resolution precedence
// (resolve_theme_with_config):
//
//	flagTheme > cfgTheme > (cfgThemeDark/cfgThemeLight by appearance) >
//	either theme_dark/theme_light alone (with a warning if an appearance
//	was also set) > appearance default (Dark/Light, or system detection).
//
// Empty strings mean "not configured". flagAppearance and cfgAppearance
// use AppearanceUnset for "not configured"; flagAppearance wins over
// cfgAppearance, and both unset means system. systemIsDark is the injected
// system/terminal darkness probe (OSC-11 or OS detection, wired by the
// caller); nil defaults to dark, matching tuicr's unwrap_or(true).
//
// Theme names resolve through ResolveNamed: bundled names first, then
// local themes from themesDir/<name>.toml (themesDir may be empty to
// disable local themes). Unknown or broken config theme names produce
// warnings and fall through; an unknown or broken flagTheme is a hard
// error (the only error case).
func Resolve(
	flagTheme, cfgTheme, cfgThemeDark, cfgThemeLight string,
	flagAppearance, cfgAppearance Appearance,
	systemIsDark func() bool,
	themesDir string,
) (*Theme, []string, error) {
	var warnings []string

	appearanceSet := flagAppearance != AppearanceUnset || cfgAppearance != AppearanceUnset
	appearance := flagAppearance
	if appearance == AppearanceUnset {
		appearance = cfgAppearance
	}
	if appearance == AppearanceUnset {
		appearance = AppearanceSystem
	}

	systemDark := func() bool {
		if systemIsDark == nil {
			return true
		}
		return systemIsDark()
	}

	warnUnknown := func(key, value string) {
		warnings = append(warnings, fmt.Sprintf(
			"Warning: Unknown theme '%s' in config key '%s', ignoring. Bundled themes: %s",
			value, key, builtinNamesDisplay()))
	}

	// resolveConfigKey resolves a theme_dark/theme_light value, demoting
	// not-found and load failures to warnings.
	resolveConfigKey := func(key, value string) *Theme {
		t, themeWarnings, err := ResolveNamed(value, themesDir)
		warnings = append(warnings, themeWarnings...)
		switch {
		case err == nil:
			return t
		case errors.Is(err, ErrLocalThemeNotFound):
			warnUnknown(key, value)
		default:
			warnings = append(warnings, fmt.Sprintf(
				"Warning: Failed to load theme '%s' from config key '%s': %v", value, key, err))
		}
		return nil
	}

	var themeDark, themeLight *Theme
	if cfgThemeDark != "" {
		themeDark = resolveConfigKey("theme_dark", cfgThemeDark)
	}
	if cfgThemeLight != "" {
		themeLight = resolveConfigKey("theme_light", cfgThemeLight)
	}

	warnAppearanceIgnored := func() {
		if appearanceSet {
			warnings = append(warnings,
				"Warning: Appearance setting is ignored when theme is explicitly set")
		}
	}

	if flagTheme != "" {
		t, themeWarnings, err := ResolveNamed(flagTheme, themesDir)
		warnings = append(warnings, themeWarnings...)
		switch {
		case errors.Is(err, ErrLocalThemeNotFound):
			return nil, warnings, fmt.Errorf(
				"unknown theme '%s', bundled themes: %s. Local themes are loaded from %s",
				flagTheme, builtinNamesDisplay(), themesDir)
		case err != nil:
			return nil, warnings, err
		}
		warnAppearanceIgnored()
		return t, warnings, nil
	}

	if cfgTheme != "" {
		t, themeWarnings, err := ResolveNamed(cfgTheme, themesDir)
		warnings = append(warnings, themeWarnings...)
		switch {
		case err == nil:
			warnAppearanceIgnored()
			return t, warnings, nil
		case errors.Is(err, ErrLocalThemeNotFound):
			warnings = append(warnings, fmt.Sprintf(
				"Warning: Unknown theme '%s' in config, using appearance mode. Bundled themes: %s",
				cfgTheme, builtinNamesDisplay()))
		default:
			warnings = append(warnings, fmt.Sprintf(
				"Warning: Failed to load theme '%s' from config: %v", cfgTheme, err))
		}
	}

	switch {
	case themeDark != nil && themeLight != nil:
		switch appearance {
		case AppearanceDark:
			return themeDark, warnings, nil
		case AppearanceLight:
			return themeLight, warnings, nil
		default: // AppearanceSystem
			if systemDark() {
				return themeDark, warnings, nil
			}
			return themeLight, warnings, nil
		}
	case themeDark != nil:
		if appearanceSet {
			warnings = append(warnings,
				"Warning: Appearance setting is ignored when only theme_dark is configured")
		}
		return themeDark, warnings, nil
	case themeLight != nil:
		if appearanceSet {
			warnings = append(warnings,
				"Warning: Appearance setting is ignored when only theme_light is configured")
		}
		return themeLight, warnings, nil
	}

	switch appearance {
	case AppearanceDark:
		return Dark(), warnings, nil
	case AppearanceLight:
		return Light(), warnings, nil
	default: // AppearanceSystem
		if systemDark() {
			return Dark(), warnings, nil
		}
		return Light(), warnings, nil
	}
}
