package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/otnc/meltool/internal/lifecycle"
	"github.com/otnc/meltool/internal/versions"
)

const pinLong = `有効なバージョンを固定します。通信しません。固定している間は、use と rollback で別のバージョンに切り替えません。同じバージョンの再適用と、recover での復旧は止めません。

固定できるのは有効なバージョンだけです。バージョンを指定するときは、有効なバージョンと合う指定だけを受け付けます。別のバージョンを固定するなら、先に "meltool use <tag>" で切り替えてから固定します。

切り替えるには "meltool unpin" で解除するか、その操作だけ --override-pin を付けます。--override-pin で切り替えたら、固定は新しいバージョンに移ります。--yes では固定を越えません。`

func addOverridePinFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("override-pin", false, "この操作に限って、バージョンの固定を越えて切り替える。固定は新しいバージョンに移る")
}

func runPin(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		lock, err := lifecycle.AcquireLock(root)
		if err != nil {
			return err
		}
		defer lock.Release()
		// 中断した変更が残っていると、どのバージョンが有効なのかが決まっていない
		if journal, err := lifecycle.LoadJournal(root); err != nil {
			return err
		} else if journal != nil {
			return lifecycle.ErrPendingJournal
		}

		state, err := lifecycle.LoadState(root)
		if err != nil {
			return err
		}
		if state.Active == nil {
			return lifecycle.ErrNoActive
		}
		if len(args) > 0 {
			query, err := versions.ParseQuery(args[0], false)
			if err != nil {
				return usageError{err}
			}
			if _, ok := versions.Select([]versions.Candidate{{Tag: state.Active.Tag}}, query); !ok {
				return fmt.Errorf("有効なバージョンは %s で、%s に合わないので固定しません。別のバージョンを固定するなら、先に \"meltool use %s\" で切り替えてください", state.Active.Tag, query.Describe(), args[0])
			}
		}

		pin, err := lifecycle.Pin(root, env.Now())
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd.OutOrStdout(), map[string]any{"pin": pin})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s) に固定しました。\n", pin.Tag, pin.StoreID)
		return nil
	}
}

func runUnpin(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		lock, err := lifecycle.AcquireLock(root)
		if err != nil {
			return err
		}
		defer lock.Release()

		removed, err := lifecycle.Unpin(root)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if asJSON {
			return writeJSON(out, map[string]any{"unpinned": removed})
		}
		if !removed {
			fmt.Fprintln(out, "バージョンを固定していません。")
			return nil
		}
		fmt.Fprintln(out, "バージョンの固定を解除しました。有効なバージョンは変えていません。")
		return nil
	}
}
