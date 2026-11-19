// Package bindings prints the multiplexer key bindings that call
// `gh dash slot N <action>`. Neither herdr nor tmux bindings can pass an
// argument, so there is one binding per slot and action.
package bindings

import (
	"fmt"
	"strings"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/handoff"
	"github.com/dlvhdr/gh-dash/v4/internal/inbox"
)

const (
	slotPlaceholder = "{n}"
	popupWidth      = "70%"
	popupHeight     = "40%"

	// SlotEnv carries the slot number to a herdr plugin popup pane, which is
	// started from a manifest command that cannot take arguments.
	SlotEnv = "GH_DASH_SLOT"
)

// Options controls the generated snippets.
type Options struct {
	Keys config.SlotKeysConfig
	// Bin is how gh-dash is invoked, "gh dash" by default.
	Bin string
	// StateDir, when set, is passed to every slot command so it reads the
	// same slots.json as the panel.
	StateDir string
	// HerdrPlugin, when set, makes the herdr bindings open the popup panes
	// of that plugin instead of running the slot command directly.
	HerdrPlugin string
}

type action struct {
	name  string
	chord string
	verb  string
}

func (o Options) actions() []action {
	return []action{
		{"agent", o.Keys.Agent, "hand slot %d to an agent"},
		{"done", o.Keys.Done, "mark slot %d as done"},
		{"open", o.Keys.Open, "open slot %d in the browser"},
	}
}

func (o Options) slotCommand(n int, name string) string {
	bin := o.Bin
	if bin == "" {
		bin = "gh dash"
	}
	command := fmt.Sprintf("%s slot %d --%s", bin, n, name)
	if o.StateDir != "" {
		command += " --state-dir " + handoff.ShellQuote(o.StateDir)
	}
	return command
}

func chordFor(chord string, n int) (string, error) {
	if !strings.Contains(chord, slotPlaceholder) {
		return "", fmt.Errorf("slot key %q must contain %s", chord, slotPlaceholder)
	}
	return strings.ReplaceAll(chord, slotPlaceholder, fmt.Sprint(n)), nil
}

func tomlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// Herdr returns [[keys.command]] entries for herdr's config.toml.
func Herdr(o Options) (string, error) {
	var b strings.Builder
	b.WriteString("# gh-dash slots: paste into herdr's config.toml, then `herdr server reload-config`\n")
	for _, a := range o.actions() {
		if a.chord == "" {
			continue
		}
		for n := 1; n <= inbox.MaxSlots; n++ {
			chord, err := chordFor(a.chord, n)
			if err != nil {
				return "", err
			}
			b.WriteString("\n[[keys.command]]\n")
			fmt.Fprintf(&b, "key = %s\n", tomlString(chord))
			if o.HerdrPlugin != "" {
				b.WriteString("type = \"shell\"\n")
				fmt.Fprintf(&b, "command = %s\n", tomlString(fmt.Sprintf(
					`"${HERDR_BIN_PATH:-herdr}" plugin pane open --plugin %s --entrypoint slot-%s --env %s=%d`,
					o.HerdrPlugin, a.name, SlotEnv, n,
				)))
			} else {
				b.WriteString("type = \"popup\"\n")
				fmt.Fprintf(&b, "command = %s\n", tomlString(o.slotCommand(n, a.name)))
				fmt.Fprintf(&b, "width = %s\nheight = %s\n", tomlString(popupWidth), tomlString(popupHeight))
			}
			fmt.Fprintf(&b, "description = %s\n", tomlString("gh-dash: "+fmt.Sprintf(a.verb, n)))
		}
	}
	return b.String(), nil
}

// TmuxKey converts a chord such as prefix+alt+3 into a tmux key table and key
// (prefix, M-3). Without prefix+ the binding goes in the root table.
func TmuxKey(chord string) (table string, key string, err error) {
	table = "root"
	parts := strings.Split(chord, "+")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", "", fmt.Errorf("invalid key %q", chord)
		}
		if i == len(parts)-1 {
			key += part
			break
		}
		switch strings.ToLower(part) {
		case "prefix":
			table = "prefix"
		case "alt", "meta", "opt", "option":
			key += "M-"
		case "ctrl", "control":
			key += "C-"
		case "shift":
			key += "S-"
		default:
			return "", "", fmt.Errorf("unknown modifier %q in key %q", part, chord)
		}
	}
	return table, key, nil
}

func tmuxString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(s) + `"`
}

// Tmux returns bind-key lines for tmux.conf. Each binding opens a popup so
// the item title is visible before anything happens.
func Tmux(o Options) (string, error) {
	var b strings.Builder
	b.WriteString("# gh-dash slots: paste into tmux.conf, then `tmux source-file` it\n")
	for _, a := range o.actions() {
		if a.chord == "" {
			continue
		}
		for n := 1; n <= inbox.MaxSlots; n++ {
			chord, err := chordFor(a.chord, n)
			if err != nil {
				return "", err
			}
			table, key, err := TmuxKey(chord)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(
				&b,
				"bind-key -T %s %s display-popup -E -w %s -h %s -d \"#{pane_current_path}\" %s\n",
				table, key, popupWidth, popupHeight, tmuxString(o.slotCommand(n, a.name)),
			)
		}
	}
	return b.String(), nil
}
