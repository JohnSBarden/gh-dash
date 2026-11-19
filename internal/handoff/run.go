package handoff

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
)

// Runner executes the commands a handoff needs.
type Runner interface {
	// Run executes argv and captures its output.
	Run(argv []string) (stdout string, stderr string, err error)
	// Exec runs argv attached to the terminal, in dir when it is not empty.
	Exec(argv []string, dir string) error
}

// OSRunner runs real processes.
type OSRunner struct{}

func (OSRunner) Run(argv []string) (string, string, error) {
	var stdout, stderr strings.Builder
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func (OSRunner) Exec(argv []string, dir string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Send hands the item and the user's prompt to an agent as configured. suffix
// makes the name of a newly started herdr agent unique. It returns a one-line
// description of what happened.
func Send(cfg config.AgentConfig, env Env, c Context, suffix string, r Runner) (string, error) {
	text, err := RenderPrompt(cfg.Prompt, c)
	if err != nil {
		return "", err
	}
	mode := cfg.Mode
	if mode == "" {
		mode = config.AgentModeNew
	}

	switch mode {
	case config.AgentModePane:
		return sendToPane(cfg, env, text, r)
	case config.AgentModeNew:
		return sendToNew(cfg, env, c, text, suffix, r)
	default:
		return "", fmt.Errorf("agent.mode must be pane or new, got %q", mode)
	}
}

func sendToPane(cfg config.AgentConfig, env Env, text string, r Runner) (string, error) {
	target := strings.TrimSpace(cfg.Pane.Target)
	if env.Multiplexer == Plain {
		return "", errors.New(
			"agent.mode pane needs herdr or tmux; use mode new in a plain terminal",
		)
	}
	if target == "" {
		return "", errors.New("agent.pane.target is not set")
	}

	switch env.Multiplexer {
	case Herdr:
		resolved := target
		if strings.HasPrefix(target, titlePrefix) {
			argv := HerdrAgentListArgv()
			out, stderr, err := r.Run(argv)
			if err != nil {
				return "", HerdrError(argv, stderr, err)
			}
			if resolved, err = ResolveHerdrTarget(target, env.Workspace, []byte(out)); err != nil {
				return "", err
			}
		}
		argv := HerdrPromptArgv(resolved, text)
		if _, stderr, err := r.Run(argv); err != nil {
			return "", HerdrError(argv, stderr, err)
		}
		return fmt.Sprintf("Sent to herdr agent %s", resolved), nil
	default:
		for _, argv := range TmuxSendArgvs(target, text) {
			if _, stderr, err := r.Run(argv); err != nil {
				return "", commandError(argv, stderr, err)
			}
		}
		return fmt.Sprintf("Sent to tmux pane %s", target), nil
	}
}

func sendToNew(
	cfg config.AgentConfig,
	env Env,
	c Context,
	text, suffix string,
	r Runner,
) (string, error) {
	cwd, err := RenderCwd(cfg.New.Cwd, c, env.Cwd, env.Home)
	if err != nil {
		return "", err
	}

	if env.Multiplexer == Herdr {
		kind := strings.TrimSpace(cfg.New.Kind)
		if kind == "" {
			return "", errors.New("agent.new.kind is not set")
		}
		argv := HerdrTabCreateArgv(env.Workspace, cwd, tabLabel(c))
		out, stderr, err := r.Run(argv)
		if err != nil {
			return "", HerdrError(argv, stderr, err)
		}
		pane, err := ParseHerdrTabCreate([]byte(out))
		if err != nil {
			return "", err
		}
		name := AgentName(c.Repo, c.Number, suffix)
		argv = HerdrAgentStartArgv(name, kind, pane)
		if _, stderr, err := r.Run(argv); err != nil {
			return "", HerdrError(argv, stderr, err)
		}
		argv = HerdrPromptArgv(name, text)
		if _, stderr, err := r.Run(argv); err != nil {
			return "", HerdrError(argv, stderr, err)
		}
		return fmt.Sprintf("Started %s agent %s in a new tab", kind, name), nil
	}

	if strings.TrimSpace(cfg.New.Command) == "" {
		return "", errors.New("agent.new.command is not set")
	}
	commandContext := c
	commandContext.Prompt = text
	command, err := Render("new.command", cfg.New.Command, commandContext)
	if err != nil {
		return "", err
	}

	if env.Multiplexer == Tmux {
		argv := TmuxNewWindowArgv(cwd, command)
		if _, stderr, err := r.Run(argv); err != nil {
			return "", commandError(argv, stderr, err)
		}
		return "Started the agent in a new tmux window", nil
	}
	if err := r.Exec(PlainArgv(command), cwd); err != nil {
		return "", fmt.Errorf("agent command failed: %w", err)
	}
	return "Agent command finished", nil
}

func tabLabel(c Context) string {
	if c.Number > 0 {
		return fmt.Sprintf("%s #%d", c.Repo, c.Number)
	}
	return c.Title
}

func commandError(argv []string, stderr string, err error) error {
	detail := strings.TrimSpace(stderr)
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("%s failed: %s", strings.Join(argv[:min(2, len(argv))], " "), detail)
}
