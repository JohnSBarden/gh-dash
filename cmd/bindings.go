package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dlvhdr/gh-dash/v4/internal/bindings"
)

var bindingsCmd = &cobra.Command{
	Use:   "bindings (--herdr | --tmux)",
	Short: "Print multiplexer key bindings for the inbox slots",
	Long: `Print key binding snippets that run "gh dash slot N <action>" for slots 1-9.

Nothing is written to your configuration: paste the output where you want it.
The chords come from slots.keys in the gh-dash config ({n} is the slot
number); by default only the agent action is bound, to prefix+alt+1..9.`,
	Example: `
# herdr: append to config.toml
gh dash bindings --herdr

# tmux
gh dash bindings --tmux >> ~/.tmux.conf

# herdr, when the panel runs as the bundled plugin
gh dash bindings --herdr --plugin gh-dash
	`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadSubcommandConfig()
		if err != nil {
			return err
		}
		opts := bindings.Options{Keys: cfg.Slots.Keys}
		opts.Bin, _ = cmd.Flags().GetString("bin")
		opts.StateDir, _ = cmd.Flags().GetString("state-dir")
		opts.HerdrPlugin, _ = cmd.Flags().GetString("plugin")

		generate := bindings.Tmux
		if herdr, _ := cmd.Flags().GetBool("herdr"); herdr {
			generate = bindings.Herdr
		}
		out, err := generate(opts)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), out)
		return nil
	},
}

func init() {
	bindingsCmd.Flags().Bool("herdr", false, "print [[keys.command]] entries for herdr's config.toml")
	bindingsCmd.Flags().Bool("tmux", false, "print bind-key lines for tmux.conf")
	bindingsCmd.MarkFlagsMutuallyExclusive("herdr", "tmux")
	bindingsCmd.MarkFlagsOneRequired("herdr", "tmux")
	bindingsCmd.Flags().String("bin", "gh dash", "how the bindings invoke gh-dash")
	bindingsCmd.Flags().String("state-dir", "", "pass this state directory to every slot command")
	bindingsCmd.Flags().String("plugin", "", "herdr only: open the popup panes of this plugin id instead")
	rootCmd.AddCommand(bindingsCmd)
}
