// Package handoff sends an inbox item plus a prompt to an AI agent. It is
// agent agnostic: under herdr it uses herdr's agent commands, under tmux it
// types into a pane or opens a window, and in a plain terminal it runs a
// command. Every external command is built by a pure function.
package handoff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
)

// Multiplexer is the environment the handoff runs in.
type Multiplexer string

const (
	Herdr Multiplexer = "herdr"
	Tmux  Multiplexer = "tmux"
	Plain Multiplexer = "plain"
)

// Env describes where the slot command was invoked.
type Env struct {
	Multiplexer Multiplexer
	// Workspace is the current herdr workspace id, if known.
	Workspace string
	// Cwd is the working directory new agents start in by default.
	Cwd string
	// Home is used to expand a leading ~ in a configured cwd.
	Home string
}

// DetectEnv picks the adapter: herdr when $HERDR_ENV (or the socket path herdr
// gives to key-bound commands) is set, else tmux when $TMUX is set, else a
// plain terminal. cwd is the caller's working directory.
func DetectEnv(getenv func(string) string, cwd, home string) Env {
	env := Env{Multiplexer: Plain, Cwd: cwd, Home: home}
	switch {
	case getenv("HERDR_ENV") != "" || getenv("HERDR_SOCKET_PATH") != "":
		env.Multiplexer = Herdr
		env.Workspace = firstNonEmpty(getenv("HERDR_WORKSPACE_ID"), getenv("HERDR_ACTIVE_WORKSPACE_ID"))
		// A popup runs outside any pane; herdr passes the cwd of the pane
		// that was active when the key was pressed.
		env.Cwd = firstNonEmpty(getenv("HERDR_ACTIVE_PANE_CWD"), cwd)
	case getenv("TMUX") != "":
		env.Multiplexer = Tmux
	}
	return env
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Context is what the prompt, command and cwd templates can use.
type Context struct {
	// Prompt is the text the user typed. In the command template it is the
	// fully rendered prompt instead.
	Prompt  string
	Title   string
	URL     string
	Repo    string
	Number  int
	Type    string
	Reasons []string
}

// ShellQuote quotes s as a single POSIX shell word.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var templateFuncs = template.FuncMap{
	"shellquote": ShellQuote,
	"join":       func(sep string, values []string) string { return strings.Join(values, sep) },
}

// Render executes a handoff template.
func Render(name, text string, c Context) (string, error) {
	tmpl, err := template.New(name).Funcs(templateFuncs).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("agent.%s template: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, c); err != nil {
		return "", fmt.Errorf("agent.%s template: %w", name, err)
	}
	return buf.String(), nil
}

// RenderPrompt builds the text handed to the agent.
func RenderPrompt(tmpl string, c Context) (string, error) {
	if strings.TrimSpace(tmpl) == "" {
		tmpl = config.DefaultAgentPrompt
	}
	text, err := Render("prompt", tmpl, c)
	return strings.TrimSpace(text), err
}

// RenderCwd resolves the working directory for a new agent: the configured
// template (with a leading ~ expanded) or the fallback when it is empty.
func RenderCwd(tmpl string, c Context, fallback, home string) (string, error) {
	if strings.TrimSpace(tmpl) == "" {
		return fallback, nil
	}
	dir, err := Render("new.cwd", tmpl, c)
	if err != nil {
		return "", err
	}
	dir = strings.TrimSpace(dir)
	if dir == "~" {
		return home, nil
	}
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		return filepath.Join(home, rest), nil
	}
	return dir, nil
}

// --- herdr -----------------------------------------------------------------

func HerdrAgentListArgv() []string {
	return []string{"herdr", "agent", "list"}
}

func HerdrPromptArgv(target, text string) []string {
	return []string{"herdr", "agent", "prompt", target, text}
}

func HerdrTabCreateArgv(workspace, cwd, label string) []string {
	argv := []string{"herdr", "tab", "create"}
	if workspace != "" {
		argv = append(argv, "--workspace", workspace)
	}
	if cwd != "" {
		argv = append(argv, "--cwd", cwd)
	}
	if label != "" {
		argv = append(argv, "--label", label)
	}
	return append(argv, "--focus")
}

func HerdrAgentStartArgv(name, kind, pane string) []string {
	return []string{"herdr", "agent", "start", name, "--kind", kind, "--pane", pane}
}

type herdrAgent struct {
	Name          string `json:"name"`
	PaneID        string `json:"pane_id"`
	WorkspaceID   string `json:"workspace_id"`
	Title         string `json:"terminal_title"`
	TitleStripped string `json:"terminal_title_stripped"`
}

const titlePrefix = "title:"

// ResolveHerdrTarget turns a configured pane target into something
// `herdr agent prompt` accepts. Agent names and pane ids pass through;
// title:<substring> is matched case-insensitively against the terminal titles
// of the agents in the given workspace (any workspace when it is empty).
func ResolveHerdrTarget(target, workspace string, agentListJSON []byte) (string, error) {
	want, isTitle := strings.CutPrefix(target, titlePrefix)
	if !isTitle {
		return target, nil
	}
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return "", errors.New("agent.pane.target: title: needs a substring to match")
	}

	var list struct {
		Result struct {
			Agents []herdrAgent `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(agentListJSON, &list); err != nil {
		return "", fmt.Errorf("could not read `herdr agent list` output: %w", err)
	}
	for _, agent := range list.Result.Agents {
		if workspace != "" && agent.WorkspaceID != workspace {
			continue
		}
		title := strings.ToLower(agent.TitleStripped + "\n" + agent.Title)
		if strings.Contains(title, want) && agent.PaneID != "" {
			return agent.PaneID, nil
		}
	}
	where := "any workspace"
	if workspace != "" {
		where = "workspace " + workspace
	}
	return "", fmt.Errorf("no agent pane with a title containing %q in %s", want, where)
}

// ParseHerdrTabCreate extracts the pane of a freshly created tab.
func ParseHerdrTabCreate(out []byte) (string, error) {
	var res struct {
		Result struct {
			RootPane struct {
				PaneID string `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return "", fmt.Errorf("could not read `herdr tab create` output: %w", err)
	}
	if res.Result.RootPane.PaneID == "" {
		return "", errors.New("`herdr tab create` did not return a pane id")
	}
	return res.Result.RootPane.PaneID, nil
}

// ErrAgentBlocked means herdr refused the prompt because the agent is waiting
// at an approval or question dialog. Nothing was sent and nothing is retried.
var ErrAgentBlocked = errors.New(
	"the agent is blocked on an approval or question, nothing was sent; answer it and try again",
)

// HerdrError turns a failed herdr call into an error worth showing.
func HerdrError(argv []string, stderr string, err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(stderr, "agent_blocked") {
		return ErrAgentBlocked
	}
	detail := strings.TrimSpace(stderr)
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(detail), &payload) == nil && payload.Error.Message != "" {
		detail = strings.TrimSpace(payload.Error.Code + ": " + payload.Error.Message)
	}
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("%s failed: %s", strings.Join(argv[:min(3, len(argv))], " "), detail)
}

var nonNameChars = regexp.MustCompile(`[^a-z0-9]+`)

// AgentName builds a herdr agent name ([a-z][a-z0-9_-]{0,31}) for an item.
// suffix keeps names unique among live agents.
func AgentName(repo string, number int, suffix string) string {
	short := repo
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		short = repo[i+1:]
	}
	short = strings.Trim(nonNameChars.ReplaceAllString(strings.ToLower(short), "-"), "-")
	tail := ""
	if number > 0 {
		tail = fmt.Sprintf("-%d", number)
	}
	if suffix = strings.Trim(nonNameChars.ReplaceAllString(strings.ToLower(suffix), "-"), "-"); suffix != "" {
		tail += "-" + suffix
	}
	const maxLen = 32
	room := maxLen - len("gd-") - len(tail)
	if room < 1 {
		tail, room = "", maxLen-len("gd-")
	}
	if len(short) > room {
		short = strings.Trim(short[:room], "-")
	}
	if short == "" {
		short = "x"
	}
	return "gd-" + short + tail
}

// --- tmux ------------------------------------------------------------------

const tmuxBuffer = "gh-dash-slot"

// TmuxSendArgvs types text into a pane and presses Enter. A single line is
// sent literally with send-keys; text with line breaks goes through a
// bracketed paste so the breaks do not submit the prompt early.
func TmuxSendArgvs(target, text string) [][]string {
	enter := []string{"tmux", "send-keys", "-t", target, "Enter"}
	if !strings.Contains(text, "\n") {
		return [][]string{
			{"tmux", "send-keys", "-t", target, "-l", "--", text},
			enter,
		}
	}
	return [][]string{
		{"tmux", "set-buffer", "-b", tmuxBuffer, "--", text},
		{"tmux", "paste-buffer", "-p", "-d", "-b", tmuxBuffer, "-t", target},
		enter,
	}
}

func TmuxNewWindowArgv(cwd, command string) []string {
	argv := []string{"tmux", "new-window"}
	if cwd != "" {
		argv = append(argv, "-c", cwd)
	}
	return append(argv, command)
}

// --- plain -----------------------------------------------------------------

func PlainArgv(command string) []string {
	return []string{"sh", "-c", command}
}
