package config

import (
	"os"

	"github.com/cli/go-gh/v2/pkg/auth"
)

// InboxConfig configures the merged Inbox view.
type InboxConfig struct {
	// Groups is the order of the blocks listed after the pins. Valid names:
	// review_requested, mine, reviewed, notifications.
	Groups []string `yaml:"groups,omitempty"`
	// Limit is the maximum number of items fetched per source.
	Limit int `yaml:"limit,omitempty" validate:"gte=0,lte=100"`
}

// AgentPaneConfig targets an agent that is already running.
type AgentPaneConfig struct {
	// Target is a herdr agent name, a herdr pane id or title:<substring>
	// (matched within the current herdr workspace), or a tmux target-pane.
	Target string `yaml:"target,omitempty"`
}

// AgentNewConfig starts a fresh agent for each handoff.
type AgentNewConfig struct {
	// Kind is the herdr agent kind (claude, codex, gemini, ...).
	Kind string `yaml:"kind,omitempty"`
	// Command is a Go template run under tmux or in a plain terminal.
	Command string `yaml:"command,omitempty"`
	// Cwd is a Go template for the working directory, empty means the
	// current one.
	Cwd string `yaml:"cwd,omitempty"`
}

// AgentConfig configures where `gh dash slot N --agent` sends an item.
type AgentConfig struct {
	Mode   string          `yaml:"mode,omitempty"   validate:"omitempty,oneof=pane new"`
	Pane   AgentPaneConfig `yaml:"pane,omitempty"`
	New    AgentNewConfig  `yaml:"new,omitempty"`
	Prompt string          `yaml:"prompt,omitempty"`
}

// SlotKeysConfig holds the chords printed by `gh dash bindings`. {n} is
// replaced by the slot number; an empty chord skips the action.
type SlotKeysConfig struct {
	Agent string `yaml:"agent,omitempty"`
	Done  string `yaml:"done,omitempty"`
	Open  string `yaml:"open,omitempty"`
}

type SlotsConfig struct {
	Keys SlotKeysConfig `yaml:"keys,omitempty"`
}

const (
	AgentModePane = "pane"
	AgentModeNew  = "new"

	DefaultAgentPrompt = "{{ .Prompt }}\n\n{{ .Title }} — {{ .URL }}\n"
)

func defaultInboxConfig() InboxConfig {
	return InboxConfig{
		Groups: []string{"review_requested", "mine", "reviewed", "notifications"},
		Limit:  50,
	}
}

func defaultAgentConfig() AgentConfig {
	return AgentConfig{
		Mode: AgentModeNew,
		New: AgentNewConfig{
			Kind:    "claude",
			Command: "claude {{ .Prompt | shellquote }}",
		},
		Prompt: DefaultAgentPrompt,
	}
}

func defaultSlotsConfig() SlotsConfig {
	return SlotsConfig{Keys: SlotKeysConfig{Agent: "prefix+alt+{n}"}}
}

// ResolveHost returns the single GitHub host this instance talks to: the
// configured host, else GH_HOST, else the default host of the gh CLI.
func (cfg Config) ResolveHost() string {
	if cfg.Host != "" {
		return cfg.Host
	}
	if host := os.Getenv("GH_HOST"); host != "" {
		return host
	}
	host, _ := auth.DefaultHost()
	return host
}

// ApplyHost makes a configured host the one used by every API client and gh
// subprocess of this process.
func (cfg Config) ApplyHost() {
	if cfg.Host != "" {
		os.Setenv("GH_HOST", cfg.Host)
	}
}
