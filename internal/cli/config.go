package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/config"
)

func runConfigShow(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		items, err := config.Items(root)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if asJSON {
			return writeJSON(out, map[string]any{"settings": items})
		}
		rows := [][]string{{"項目", "値", "既定", "説明"}}
		for _, item := range items {
			rows = append(rows, []string{item.Key, item.Value, item.Default, item.Description})
		}
		writeTable(out, rows)
		return nil
	}
}

func runConfigSet(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		if err := config.Set(root, args[0], args[1]); err != nil {
			if errors.Is(err, config.ErrInvalid) {
				return usageError{err}
			}
			return err
		}
		if asJSON {
			return writeJSON(cmd.OutOrStdout(), map[string]any{"key": args[0], "value": args[1]})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s を %s にしました。\n", args[0], args[1])
		return nil
	}
}
