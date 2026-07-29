package theme

import (
	"strings"
	"testing"
)

func TestParseAppearance(t *testing.T) {
	tests := []struct {
		in      string
		want    Appearance
		wantErr bool
	}{
		{"dark", AppearanceDark, false},
		{"light", AppearanceLight, false},
		{"system", AppearanceSystem, false},
		{" Dark ", AppearanceDark, false},
		{"SYSTEM", AppearanceSystem, false},
		{"", AppearanceUnset, true},
		{"auto", AppearanceUnset, true},
	}
	for _, tc := range tests {
		got, err := ParseAppearance(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseAppearance(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("ParseAppearance(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestAppearanceString(t *testing.T) {
	tests := map[Appearance]string{
		AppearanceDark:   "dark",
		AppearanceLight:  "light",
		AppearanceSystem: "system",
		AppearanceUnset:  "unset",
	}
	for a, want := range tests {
		if got := a.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", a, got, want)
		}
	}
}

func alwaysDark() bool  { return true }
func alwaysLight() bool { return false }

func TestResolvePrecedence(t *testing.T) {
	tests := []struct {
		name string

		flagTheme, cfgTheme, cfgThemeDark, cfgThemeLight string
		flagAppearance, cfgAppearance                    Appearance
		systemIsDark                                     func() bool

		wantTheme    string
		wantWarnings []string
		wantErr      string
	}{
		{
			name:      "flag theme wins over everything",
			flagTheme: "tokyo-night-storm", cfgTheme: "light",
			cfgThemeDark: "dark", cfgThemeLight: "light",
			wantTheme: "tokyo-night-storm",
		},
		{
			name:      "flag theme with appearance warns",
			flagTheme: "dark", flagAppearance: AppearanceLight,
			wantTheme:    "dark",
			wantWarnings: []string{"Appearance setting is ignored when theme is explicitly set"},
		},
		{
			name:      "flag theme with config appearance warns",
			flagTheme: "dark", cfgAppearance: AppearanceSystem,
			wantTheme:    "dark",
			wantWarnings: []string{"Appearance setting is ignored when theme is explicitly set"},
		},
		{
			name:      "unknown flag theme is a hard error",
			flagTheme: "nope",
			wantErr:   "unknown theme 'nope'",
		},
		{
			name:      "config theme used when no flag",
			cfgTheme:  "tokyo-night-day",
			wantTheme: "tokyo-night-day",
		},
		{
			name:     "config theme with appearance warns",
			cfgTheme: "tokyo-night-day", flagAppearance: AppearanceDark,
			wantTheme:    "tokyo-night-day",
			wantWarnings: []string{"Appearance setting is ignored when theme is explicitly set"},
		},
		{
			name:     "unknown config theme warns and falls to appearance",
			cfgTheme: "nope", flagAppearance: AppearanceLight,
			wantTheme:    "light",
			wantWarnings: []string{"Unknown theme 'nope' in config, using appearance mode"},
		},
		{
			name:         "dark/light pair follows dark appearance",
			cfgThemeDark: "tokyo-night-storm", cfgThemeLight: "tokyo-night-day",
			flagAppearance: AppearanceDark,
			wantTheme:      "tokyo-night-storm",
		},
		{
			name:         "dark/light pair follows light appearance",
			cfgThemeDark: "tokyo-night-storm", cfgThemeLight: "tokyo-night-day",
			flagAppearance: AppearanceLight,
			wantTheme:      "tokyo-night-day",
		},
		{
			name:         "dark/light pair follows system detection dark",
			cfgThemeDark: "tokyo-night-storm", cfgThemeLight: "tokyo-night-day",
			systemIsDark: alwaysDark,
			wantTheme:    "tokyo-night-storm",
		},
		{
			name:         "dark/light pair follows system detection light",
			cfgThemeDark: "tokyo-night-storm", cfgThemeLight: "tokyo-night-day",
			systemIsDark: alwaysLight,
			wantTheme:    "tokyo-night-day",
		},
		{
			name:         "flag appearance beats config appearance",
			cfgThemeDark: "tokyo-night-storm", cfgThemeLight: "tokyo-night-day",
			flagAppearance: AppearanceLight, cfgAppearance: AppearanceDark,
			wantTheme: "tokyo-night-day",
		},
		{
			name:         "theme_dark alone without appearance",
			cfgThemeDark: "tokyo-night-storm",
			systemIsDark: alwaysLight,
			wantTheme:    "tokyo-night-storm",
		},
		{
			name:         "theme_dark alone with appearance warns",
			cfgThemeDark: "tokyo-night-storm", cfgAppearance: AppearanceLight,
			wantTheme:    "tokyo-night-storm",
			wantWarnings: []string{"Appearance setting is ignored when only theme_dark is configured"},
		},
		{
			name:          "theme_light alone with appearance warns",
			cfgThemeLight: "tokyo-night-day", flagAppearance: AppearanceDark,
			wantTheme:    "tokyo-night-day",
			wantWarnings: []string{"Appearance setting is ignored when only theme_light is configured"},
		},
		{
			name:         "unknown theme_dark warns and drops the slot",
			cfgThemeDark: "nope", cfgThemeLight: "tokyo-night-day",
			flagAppearance: AppearanceDark,
			wantTheme:      "tokyo-night-day",
			wantWarnings: []string{
				"Unknown theme 'nope' in config key 'theme_dark', ignoring",
				"Appearance setting is ignored when only theme_light is configured",
			},
		},
		{
			name:           "unknown theme_light warns",
			cfgThemeLight:  "nope",
			flagAppearance: AppearanceLight,
			wantTheme:      "light",
			wantWarnings: []string{
				"Unknown theme 'nope' in config key 'theme_light', ignoring",
			},
		},
		{
			name:           "appearance dark default",
			flagAppearance: AppearanceDark,
			wantTheme:      "dark",
		},
		{
			name:          "config appearance light default",
			cfgAppearance: AppearanceLight,
			wantTheme:     "light",
		},
		{
			name:         "system appearance uses probe",
			systemIsDark: alwaysLight,
			wantTheme:    "light",
		},
		{
			name:      "nil probe defaults to dark",
			wantTheme: "dark",
		},
		{
			name:           "explicit system appearance uses probe",
			flagAppearance: AppearanceSystem,
			systemIsDark:   alwaysDark,
			wantTheme:      "dark",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			th, warnings, err := Resolve(
				tc.flagTheme, tc.cfgTheme, tc.cfgThemeDark, tc.cfgThemeLight,
				tc.flagAppearance, tc.cfgAppearance, tc.systemIsDark)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if th.Name != tc.wantTheme {
				t.Errorf("resolved theme = %q, want %q", th.Name, tc.wantTheme)
			}
			if len(warnings) != len(tc.wantWarnings) {
				t.Fatalf("warnings = %v, want %d entries %v", warnings, len(tc.wantWarnings), tc.wantWarnings)
			}
			for i, want := range tc.wantWarnings {
				if !strings.Contains(warnings[i], want) {
					t.Errorf("warnings[%d] = %q, want containing %q", i, warnings[i], want)
				}
			}
		})
	}
}

func TestResolveUnknownFlagThemeListsBuiltins(t *testing.T) {
	_, _, err := Resolve("wat", "", "", "", AppearanceUnset, AppearanceUnset, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dark, light, tokyo-night-storm, tokyo-night-day") {
		t.Errorf("error should list bundled themes, got: %v", err)
	}
}
