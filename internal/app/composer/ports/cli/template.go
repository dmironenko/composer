package cli

import (
	"github.com/spf13/cobra"
)

func templateCommand(bootstrap Bootstrap) *cobra.Command {
	root := &cobra.Command{Use: "template", Short: "Manage repository template overrides"}
	export := &cobra.Command{Use: "export", Short: "Export an embedded template"}

	var output string

	export.AddCommand(&cobra.Command{
		Use: "compose", Short: "Export the default Compose template without overwriting files", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return bootstrap.Export(cmd.Context(), output)
		},
	})
	export.PersistentFlags().StringVar(&output, "output", "", "Destination template file")
	root.AddCommand(export)

	return root
}
