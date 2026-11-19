package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"charm.land/log/v2"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/cli/go-gh/v2/pkg/term"
	"github.com/spf13/cobra"

	"github.com/dlvhdr/gh-dash/v4/internal/bindings"
	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/handoff"
	"github.com/dlvhdr/gh-dash/v4/internal/inbox"
	"github.com/dlvhdr/gh-dash/v4/internal/slotcmd"
)

var slotCmd = &cobra.Command{
	Use:   "slot <N> (--agent | --done | --open)",
	Short: "Act on the item in inbox slot N from outside the panel",
	Long: `Act on the item shown in slot N (1-9) of the Inbox view.

The running panel exports its first nine rows to slots.json; this command
reads that file, so the panel does not need to be focused. Bind it to a
multiplexer key with "gh dash bindings".`,
	Example: `
# Ask for a prompt, then hand slot 3 and the prompt to the configured agent
gh dash slot 3 --agent

# Mark slot 1 as done without asking
gh dash slot 1 --done --yes

# Open slot 2 in the browser
gh dash slot 2 --open
	`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSlot,
}

// loadSubcommandConfig reads the same configuration as the panel and applies
// its host, without the panel's logging.
func loadSubcommandConfig() (config.Config, error) {
	log.SetOutput(io.Discard)
	cfg, err := config.ParseConfig(config.Location{ConfigFlag: cfgFlag})
	if err != nil {
		return cfg, err
	}
	cfg.ApplyHost()
	return cfg, nil
}

func slotNumber(args []string) (int, error) {
	raw := os.Getenv(bindings.SlotEnv)
	if len(args) > 0 {
		raw = args[0]
	}
	if raw == "" {
		return 0, errors.New("which slot? pass a number from 1 to 9")
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("slot must be a number from 1 to 9, got %q", raw)
	}
	return n, nil
}

func runSlot(cmd *cobra.Command, args []string) error {
	opts := slotcmd.Options{}
	for _, action := range []string{slotcmd.ActionAgent, slotcmd.ActionDone, slotcmd.ActionOpen} {
		if set, _ := cmd.Flags().GetBool(action); set {
			opts.Action = action
		}
	}
	opts.Prompt, _ = cmd.Flags().GetString("prompt")
	opts.Yes, _ = cmd.Flags().GetBool("yes")

	var err error
	if opts.Slot, err = slotNumber(args); err != nil {
		return err
	}
	if stateDir, _ := cmd.Flags().GetString("state-dir"); stateDir != "" {
		os.Setenv(inbox.StateDirEnv, stateDir)
	}
	cfg, err := loadSubcommandConfig()
	if err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	interactive := term.IsTerminal(os.Stdin)
	deps := slotcmd.Deps{
		Store:       inbox.Store{Dir: inbox.DefaultStateDir(cfg.ResolveHost())},
		In:          os.Stdin,
		Out:         cmd.OutOrStdout(),
		Interactive: interactive,
		Agent:       cfg.Agent,
		Env:         handoff.DetectEnv(os.Getenv, cwd, home),
		Runner:      handoff.OSRunner{},
		MarkDone:    data.MarkNotificationDone,
		Open: func(url string) error {
			return browser.New("", io.Discard, io.Discard).Browse(url)
		},
		Now: time.Now,
	}

	err = slotcmd.Run(opts, deps)
	inPopup := interactive && deps.Env.Multiplexer != handoff.Plain
	switch {
	case errors.Is(err, slotcmd.ErrCancelled):
		fmt.Fprintln(deps.Out, "Cancelled")
		return nil
	case err != nil && inPopup:
		// A multiplexer popup closes with the command, so keep the error on
		// screen until it has been read.
		fmt.Fprintf(deps.Out, "Error: %v\n\nPress Enter to close ", err)
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(1)
	case err == nil && inPopup:
		time.Sleep(700 * time.Millisecond)
	}
	return err
}

func init() {
	slotCmd.Flags().Bool(slotcmd.ActionAgent, false, "ask for a prompt and hand the item to an agent")
	slotCmd.Flags().Bool(slotcmd.ActionDone, false, "mark the item as done")
	slotCmd.Flags().Bool(slotcmd.ActionOpen, false, "open the item in the browser")
	slotCmd.MarkFlagsMutuallyExclusive(slotcmd.ActionAgent, slotcmd.ActionDone, slotcmd.ActionOpen)
	slotCmd.MarkFlagsOneRequired(slotcmd.ActionAgent, slotcmd.ActionDone, slotcmd.ActionOpen)
	slotCmd.Flags().String("prompt", "", "prompt for --agent, instead of asking for it")
	slotCmd.Flags().BoolP("yes", "y", false, "do not ask before marking as done")
	slotCmd.Flags().String("state-dir", "", "directory holding slots.json (default: the panel's state directory)")
	rootCmd.AddCommand(slotCmd)
}
