package completion

import (
	"os"

	"github.com/spf13/cobra"
)

var CompletionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate completion script",
	Long: `To load completions:

Bash:

$ source <(modelx completion bash)

# To load completions for each session, execute once:
Linux:
  $ modelx completion bash > /etc/bash_completion.d/modelx
MacOS:
  $ modelx completion bash > /usr/local/etc/bash_completion.d/modelx

Zsh:

# If shell completion is not already enabled in your environment you will need
# to enable it.  You can execute the following once:

$ echo "autoload -U compinit; compinit" >> ~/.zshrc

# To load completions for each session, execute once:
$ modelx completion zsh > "${fpath[1]}/_modelx"

# You will need to start a new shell for this setup to take effect.

Fish:

$ modelx completion fish | source

# To load completions for each session, execute once:
$ modelx completion fish > ~/.config/fish/completions/modelx.fish
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershelxl"},
	Args:                  cobra.ExactValidArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			cmd.Root().GenBashCompletionV2(os.Stdout, true)
		case "zsh":
			cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			cmd.Root().GenPowerShellCompletion(os.Stdout)
		}
	},
}
