package handoff

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
)

var item = Context{
	Prompt:  "Review this",
	Title:   "Add gears",
	URL:     "https://github.example.com/acme/widgets/pull/7",
	Repo:    "acme/widgets",
	Number:  7,
	Type:    "PullRequest",
	Reasons: []string{"review_requested", "mention"},
}

const renderedPrompt = "Review this\n\nAdd gears — https://github.example.com/acme/widgets/pull/7"

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

type call struct {
	argv []string
	dir  string
	exec bool
}

type fakeRunner struct {
	calls   []call
	outputs map[string]string // keyed by the first three argv words
	fail    map[string]string // stderr for a failing call, same key
}

func runnerKey(argv []string) string {
	return strings.Join(argv[:min(3, len(argv))], " ")
}

func (f *fakeRunner) Run(argv []string) (string, string, error) {
	f.calls = append(f.calls, call{argv: argv})
	if stderr, ok := f.fail[runnerKey(argv)]; ok {
		return "", stderr, errors.New("exit status 1")
	}
	return f.outputs[runnerKey(argv)], "", nil
}

func (f *fakeRunner) Exec(argv []string, dir string) error {
	f.calls = append(f.calls, call{argv: argv, dir: dir, exec: true})
	return nil
}

func (f *fakeRunner) argvs() [][]string {
	out := make([][]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.argv
	}
	return out
}

const agentList = `{"id":"1","result":{"type":"agent_list","agents":[
  {"pane_id":"w1:p1","workspace_id":"w1","terminal_title":"octocat · claude agents","terminal_title_stripped":"octocat · claude agents"},
  {"pane_id":"w2:p4","workspace_id":"w2","terminal_title":"\u001b[1moctocat · Claude Agents","terminal_title_stripped":"octocat · Claude Agents"},
  {"pane_id":"w2:p5","workspace_id":"w2","name":"reviewer","terminal_title":"codex","terminal_title_stripped":"codex"}
]}}`

func TestDetectEnv(t *testing.T) {
	e := DetectEnv(env(map[string]string{
		"HERDR_ENV":          "1",
		"TMUX":               "/tmp/tmux-1/default,1,0",
		"HERDR_WORKSPACE_ID": "w2",
	}), "/work", "/home/octocat")
	require.Equal(t, Env{Multiplexer: Herdr, Workspace: "w2", Cwd: "/work", Home: "/home/octocat"}, e)

	// a herdr key binding popup: no HERDR_ENV, but the socket and active ids
	e = DetectEnv(env(map[string]string{
		"HERDR_SOCKET_PATH":         "/run/herdr.sock",
		"HERDR_ACTIVE_WORKSPACE_ID": "w3",
		"HERDR_ACTIVE_PANE_CWD":     "/src/widgets",
	}), "/", "/home/octocat")
	require.Equal(t, Herdr, e.Multiplexer)
	require.Equal(t, "w3", e.Workspace)
	require.Equal(t, "/src/widgets", e.Cwd)

	require.Equal(t, Tmux, DetectEnv(env(map[string]string{"TMUX": "x"}), "/work", "").Multiplexer)
	require.Equal(t, Plain, DetectEnv(env(nil), "/work", "").Multiplexer)
}

func TestRenderPrompt(t *testing.T) {
	text, err := RenderPrompt("", item)
	require.NoError(t, err)
	require.Equal(t, renderedPrompt, text)

	text, err = RenderPrompt(`{{ .Prompt }} [{{ .Repo }}#{{ .Number }}] ({{ join ", " .Reasons }})`, item)
	require.NoError(t, err)
	require.Equal(t, "Review this [acme/widgets#7] (review_requested, mention)", text)

	_, err = RenderPrompt("{{ .Nope }}", item)
	require.ErrorContains(t, err, "agent.prompt template")
}

func TestShellQuote(t *testing.T) {
	require.Equal(t, `'it'\''s $HOME; rm -rf "x"'`, ShellQuote(`it's $HOME; rm -rf "x"`))
	require.Equal(t, "''", ShellQuote(""))
}

func TestRenderCwd(t *testing.T) {
	dir, err := RenderCwd("", item, "/fallback", "/home/octocat")
	require.NoError(t, err)
	require.Equal(t, "/fallback", dir)

	dir, err = RenderCwd("~/src/{{ .Repo }}", item, "/fallback", "/home/octocat")
	require.NoError(t, err)
	require.Equal(t, "/home/octocat/src/acme/widgets", dir)
}

func TestHerdrArgv(t *testing.T) {
	require.Equal(t, []string{"herdr", "agent", "prompt", "w1:p1", "hi"}, HerdrPromptArgv("w1:p1", "hi"))
	require.Equal(
		t,
		[]string{"herdr", "tab", "create", "--workspace", "w1", "--cwd", "/src", "--label", "acme/widgets #7", "--focus"},
		HerdrTabCreateArgv("w1", "/src", "acme/widgets #7"),
	)
	require.Equal(t, []string{"herdr", "tab", "create", "--focus"}, HerdrTabCreateArgv("", "", ""))
	require.Equal(
		t,
		[]string{"herdr", "agent", "start", "gd-widgets-7", "--kind", "codex", "--pane", "w1:p9"},
		HerdrAgentStartArgv("gd-widgets-7", "codex", "w1:p9"),
	)
}

func TestResolveHerdrTarget(t *testing.T) {
	for _, passthrough := range []string{"reviewer", "w1:p1"} {
		got, err := ResolveHerdrTarget(passthrough, "w2", nil)
		require.NoError(t, err)
		require.Equal(t, passthrough, got)
	}

	got, err := ResolveHerdrTarget("title:claude agents", "w2", []byte(agentList))
	require.NoError(t, err)
	require.Equal(t, "w2:p4", got, "only the current workspace is searched, case-insensitively")

	got, err = ResolveHerdrTarget("title:claude agents", "", []byte(agentList))
	require.NoError(t, err)
	require.Equal(t, "w1:p1", got)

	_, err = ResolveHerdrTarget("title:claude agents", "w9", []byte(agentList))
	require.ErrorContains(t, err, "workspace w9")
	_, err = ResolveHerdrTarget("title:", "w1", []byte(agentList))
	require.Error(t, err)
	_, err = ResolveHerdrTarget("title:x", "w1", []byte("not json"))
	require.Error(t, err)
}

func TestParseHerdrTabCreate(t *testing.T) {
	pane, err := ParseHerdrTabCreate([]byte(`{"result":{"tab":{"tab_id":"w1:t3"},"root_pane":{"pane_id":"w1:p9"}}}`))
	require.NoError(t, err)
	require.Equal(t, "w1:p9", pane)
	_, err = ParseHerdrTabCreate([]byte(`{"result":{}}`))
	require.Error(t, err)
}

func TestHerdrError(t *testing.T) {
	argv := HerdrPromptArgv("w1:p1", "hi")
	require.NoError(t, HerdrError(argv, "", nil))
	require.ErrorIs(
		t,
		HerdrError(argv, `{"error":{"code":"agent_blocked","message":"agent is blocked"}}`, errors.New("exit status 1")),
		ErrAgentBlocked,
	)
	err := HerdrError(argv, `{"error":{"code":"agent_not_found","message":"no such agent"}}`, errors.New("exit status 1"))
	require.EqualError(t, err, "herdr agent prompt failed: agent_not_found: no such agent")
	err = HerdrError(argv, "", errors.New("exec: herdr not found"))
	require.ErrorContains(t, err, "herdr not found")
}

func TestAgentName(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	names := []string{
		AgentName("acme/widgets", 7, "k3x"),
		AgentName("acme/Some.Very_Long-Repository-Name-That-Goes-On", 123456, "abc12"),
		AgentName("", 0, ""),
		AgentName("acme/---", 5, ""),
	}
	require.Equal(t, "gd-widgets-7-k3x", names[0])
	for _, name := range names {
		require.Regexp(t, valid, name)
	}
	require.True(t, strings.HasSuffix(names[1], "-123456-abc12"))
}

func TestTmuxArgv(t *testing.T) {
	require.Equal(t, [][]string{
		{"tmux", "send-keys", "-t", "work:1.2", "-l", "--", "-one line; $HOME"},
		{"tmux", "send-keys", "-t", "work:1.2", "Enter"},
	}, TmuxSendArgvs("work:1.2", "-one line; $HOME"))

	require.Equal(t, [][]string{
		{"tmux", "set-buffer", "-b", "gh-dash-slot", "--", "two\nlines"},
		{"tmux", "paste-buffer", "-p", "-d", "-b", "gh-dash-slot", "-t", "%3"},
		{"tmux", "send-keys", "-t", "%3", "Enter"},
	}, TmuxSendArgvs("%3", "two\nlines"))

	require.Equal(t, []string{"tmux", "new-window", "-c", "/src", "claude 'x'"}, TmuxNewWindowArgv("/src", "claude 'x'"))
	require.Equal(t, []string{"tmux", "new-window", "claude"}, TmuxNewWindowArgv("", "claude"))
	require.Equal(t, []string{"sh", "-c", "claude 'x'"}, PlainArgv("claude 'x'"))
}

func paneConfig(target string) config.AgentConfig {
	return config.AgentConfig{Mode: config.AgentModePane, Pane: config.AgentPaneConfig{Target: target}}
}

func newConfig() config.AgentConfig {
	return config.AgentConfig{
		Mode: config.AgentModeNew,
		New:  config.AgentNewConfig{Kind: "codex", Command: "claude {{ .Prompt | shellquote }}"},
	}
}

func TestSendHerdrPaneResolvesTitleInCurrentWorkspace(t *testing.T) {
	r := &fakeRunner{outputs: map[string]string{"herdr agent list": agentList}}
	summary, err := Send(paneConfig("title:claude agents"), Env{Multiplexer: Herdr, Workspace: "w2"}, item, "", r)
	require.NoError(t, err)
	require.Equal(t, [][]string{
		{"herdr", "agent", "list"},
		{"herdr", "agent", "prompt", "w2:p4", renderedPrompt},
	}, r.argvs())
	require.Contains(t, summary, "w2:p4")
}

func TestSendHerdrPaneByNameSkipsTheLookup(t *testing.T) {
	r := &fakeRunner{}
	_, err := Send(paneConfig("reviewer"), Env{Multiplexer: Herdr}, item, "", r)
	require.NoError(t, err)
	require.Equal(t, [][]string{{"herdr", "agent", "prompt", "reviewer", renderedPrompt}}, r.argvs())
}

func TestSendHerdrPaneSurfacesBlockedAgentWithoutRetrying(t *testing.T) {
	r := &fakeRunner{fail: map[string]string{
		"herdr agent prompt": `{"error":{"code":"agent_blocked","message":"blocked"}}`,
	}}
	_, err := Send(paneConfig("reviewer"), Env{Multiplexer: Herdr}, item, "", r)
	require.ErrorIs(t, err, ErrAgentBlocked)
	require.Len(t, r.calls, 1, "the prompt is not sent again")
}

func TestSendHerdrNewCreatesTabStartsAgentAndPrompts(t *testing.T) {
	r := &fakeRunner{outputs: map[string]string{
		"herdr tab create": `{"result":{"root_pane":{"pane_id":"w2:p9"}}}`,
	}}
	summary, err := Send(newConfig(), Env{Multiplexer: Herdr, Workspace: "w2", Cwd: "/src"}, item, "k3x", r)
	require.NoError(t, err)
	require.Equal(t, [][]string{
		{"herdr", "tab", "create", "--workspace", "w2", "--cwd", "/src", "--label", "acme/widgets #7", "--focus"},
		{"herdr", "agent", "start", "gd-widgets-7-k3x", "--kind", "codex", "--pane", "w2:p9"},
		{"herdr", "agent", "prompt", "gd-widgets-7-k3x", renderedPrompt},
	}, r.argvs())
	require.Contains(t, summary, "codex")
}

func TestSendHerdrNewStopsWhenTheAgentDoesNotStart(t *testing.T) {
	r := &fakeRunner{
		outputs: map[string]string{"herdr tab create": `{"result":{"root_pane":{"pane_id":"w2:p9"}}}`},
		fail:    map[string]string{"herdr agent start": `{"error":{"code":"agent_not_ready","message":"not ready"}}`},
	}
	_, err := Send(newConfig(), Env{Multiplexer: Herdr}, item, "", r)
	require.ErrorContains(t, err, "agent_not_ready")
	require.Len(t, r.calls, 2)
}

func TestSendTmuxPane(t *testing.T) {
	r := &fakeRunner{}
	cfg := paneConfig("agents:1.0")
	cfg.Prompt = "{{ .Prompt }}: {{ .URL }}"
	_, err := Send(cfg, Env{Multiplexer: Tmux}, item, "", r)
	require.NoError(t, err)
	require.Equal(t, TmuxSendArgvs("agents:1.0", "Review this: "+item.URL), r.argvs())
}

func TestSendTmuxNewWindow(t *testing.T) {
	r := &fakeRunner{}
	cfg := newConfig()
	cfg.New.Cwd = "~/src/{{ .Repo }}"
	_, err := Send(cfg, Env{Multiplexer: Tmux, Cwd: "/work", Home: "/home/octocat"}, item, "", r)
	require.NoError(t, err)
	require.Equal(t, [][]string{{
		"tmux", "new-window", "-c", "/home/octocat/src/acme/widgets",
		"claude " + ShellQuote(renderedPrompt),
	}}, r.argvs())
}

func TestSendPlain(t *testing.T) {
	r := &fakeRunner{}
	_, err := Send(newConfig(), Env{Multiplexer: Plain, Cwd: "/work"}, item, "", r)
	require.NoError(t, err)
	require.Equal(t, []call{{
		argv: []string{"sh", "-c", "claude " + ShellQuote(renderedPrompt)},
		dir:  "/work",
		exec: true,
	}}, r.calls)

	_, err = Send(paneConfig("x"), Env{Multiplexer: Plain}, item, "", &fakeRunner{})
	require.ErrorContains(t, err, "needs herdr or tmux")
}

func TestSendValidatesConfig(t *testing.T) {
	_, err := Send(paneConfig(""), Env{Multiplexer: Tmux}, item, "", &fakeRunner{})
	require.ErrorContains(t, err, "agent.pane.target")

	cfg := newConfig()
	cfg.New.Kind = ""
	_, err = Send(cfg, Env{Multiplexer: Herdr}, item, "", &fakeRunner{})
	require.ErrorContains(t, err, "agent.new.kind")

	cfg = newConfig()
	cfg.New.Command = ""
	_, err = Send(cfg, Env{Multiplexer: Plain}, item, "", &fakeRunner{})
	require.ErrorContains(t, err, "agent.new.command")

	_, err = Send(config.AgentConfig{Mode: "sideways"}, Env{}, item, "", &fakeRunner{})
	require.Error(t, err)
}
