package bindings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
)

var defaults = Options{Keys: config.SlotKeysConfig{Agent: "prefix+alt+{n}"}}

func TestHerdrDefaults(t *testing.T) {
	out, err := Herdr(defaults)
	require.NoError(t, err)
	require.Equal(t, 9, strings.Count(out, "[[keys.command]]"))
	require.Contains(t, out, `
[[keys.command]]
key = "prefix+alt+3"
type = "popup"
command = "gh dash slot 3 --agent"
width = "70%"
height = "40%"
description = "gh-dash: hand slot 3 to an agent"
`)
	require.NotContains(t, out, "--done")
}

func TestHerdrAllActionsWithStateDirAndBin(t *testing.T) {
	out, err := Herdr(Options{
		Keys: config.SlotKeysConfig{
			Agent: "prefix+alt+{n}",
			Done:  "prefix+ctrl+{n}",
			Open:  "ctrl+alt+{n}",
		},
		Bin:      "/opt/bin/gh-dash --config /etc/dash.yml",
		StateDir: "/var/state/it's here",
	})
	require.NoError(t, err)
	require.Equal(t, 27, strings.Count(out, "[[keys.command]]"))
	require.Contains(t, out, `key = "prefix+ctrl+9"`)
	require.Contains(t, out, `key = "ctrl+alt+1"`)
	require.Contains(
		t,
		out,
		`command = "/opt/bin/gh-dash --config /etc/dash.yml slot 9 --done --state-dir '/var/state/it'\\''s here'"`,
	)
}

func TestHerdrPluginBindingsOpenThePopupPane(t *testing.T) {
	opts := defaults
	opts.HerdrPlugin = "gh-dash"
	out, err := Herdr(opts)
	require.NoError(t, err)
	require.Contains(t, out, `type = "shell"`)
	require.Contains(
		t,
		out,
		`command = "\"${HERDR_BIN_PATH:-herdr}\" plugin pane open --plugin gh-dash --entrypoint slot-agent --env GH_DASH_SLOT=4"`,
	)
}

func TestTmuxKey(t *testing.T) {
	for chord, want := range map[string][2]string{
		"prefix+alt+3":   {"prefix", "M-3"},
		"alt+3":          {"root", "M-3"},
		"prefix+3":       {"prefix", "3"},
		"ctrl+alt+9":     {"root", "C-M-9"},
		"prefix+shift+F": {"prefix", "S-F"},
	} {
		table, key, err := TmuxKey(chord)
		require.NoError(t, err, chord)
		require.Equal(t, want, [2]string{table, key}, chord)
	}
	_, _, err := TmuxKey("hyper+3")
	require.Error(t, err)
	_, _, err = TmuxKey("alt+")
	require.Error(t, err)
}

func TestTmux(t *testing.T) {
	opts := defaults
	opts.Keys.Open = "alt+{n}"
	out, err := Tmux(opts)
	require.NoError(t, err)
	require.Equal(t, 18, strings.Count(out, "bind-key"))
	require.Contains(
		t,
		out,
		`bind-key -T prefix M-3 display-popup -E -w 70% -h 40% -d "#{pane_current_path}" "gh dash slot 3 --agent"`+"\n",
	)
	require.Contains(t, out, `bind-key -T root M-9 display-popup`)
	require.Contains(t, out, `"gh dash slot 9 --open"`)
}

func TestKeyMustContainThePlaceholder(t *testing.T) {
	_, err := Herdr(Options{Keys: config.SlotKeysConfig{Agent: "prefix+alt+1"}})
	require.ErrorContains(t, err, "{n}")
	_, err = Tmux(Options{Keys: config.SlotKeysConfig{Agent: "prefix+alt+1"}})
	require.Error(t, err)
}
