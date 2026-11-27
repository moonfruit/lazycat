package cli

import "github.com/spf13/cobra"

// Execute dispatches the command and preserves the VM process's exit status.
func Execute(args []string, startVM func() (int, error), provisionOIDC func(bool) error, watchOIDC func() error) (int, error) {
	var exitCode int
	runVM := func(*cobra.Command, []string) error {
		code, err := startVM()
		exitCode = code
		return err
	}
	root := &cobra.Command{
		Use:           "haos-vm",
		Short:         "Manage the LazyCat HAOS VM (default: start the VM)",
		Args:          cobra.NoArgs,
		RunE:          runVM,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	vm := &cobra.Command{
		Use:   "vm",
		Short: "Start and supervise the HAOS VM in the QEMU container",
		Args:  cobra.NoArgs,
		RunE:  runVM,
	}
	var recoverOnly, watch bool
	oidc := &cobra.Command{
		Use:   "oidc",
		Short: "Provision LazyCat OIDC inside the HAOS VM",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if watch {
				return watchOIDC()
			}
			return provisionOIDC(recoverOnly)
		},
	}
	oidc.Flags().BoolVar(&recoverOnly, "recover", false, "Recover an interrupted configuration change and exit")
	oidc.Flags().BoolVar(&watch, "watch", false, "Provision on startup and watch for restored credentials")
	oidc.MarkFlagsMutuallyExclusive("watch", "recover")
	root.AddCommand(vm, oidc)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		return 1, err
	}
	return exitCode, nil
}
