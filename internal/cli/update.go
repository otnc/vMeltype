package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/catalog"
	"github.com/otnc/vmeltype/internal/lifecycle"
	"github.com/otnc/vmeltype/internal/prompt"
	"github.com/otnc/vmeltype/internal/store"
	"github.com/otnc/vmeltype/internal/versions"
)

const updateLong = `有効版より新しい版を公式一覧から探し、保持していなければ取得してから切り替えます。切り替えは use と同じで、計画を示して確認します。

対象は有効版と同じ variant (通常版か portable 版) です。既定は正式版の最新で、--pre を付けるとプレリリースと正式版を合わせた最大を選びます。有効版より古い版には下げません。
版を固定していれば、取得する前に止めます。越えるには --override-pin を付けます。`

// errUpdateAvailable は、check が新しい版を見つけたことを表す。終了コードだけで知らせ、エラーとしては表示しない。
var errUpdateAvailable = errors.New("新しい版があります")

func setupCheckFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("pre", false, "プレリリースも含めて探す")
}

func setupUpdateFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("pre", false, "プレリリースも含めて探す")
	cmd.Flags().Bool("override-pin", false, "この一回に限って、版の固定を越えて切り替える")
	addStopAppFlag(cmd)
}

// findUpdate は、有効版より新しい版を公式一覧から探す。この端末の、有効版と同じ variant の配布物があるリリースだけを候補にする。
func findUpdate(cmd *cobra.Command, env *Env, root string) (*lifecycle.Activation, *catalog.Release, error) {
	state, err := lifecycle.LoadState(root)
	if err != nil {
		return nil, nil, err
	}
	if state.Active == nil {
		return nil, nil, errors.New("有効な版がないので、新しい版を探せません。先に \"vmeltype use <tag>\" で有効にしてください")
	}
	_, c, err := fetchCatalog(cmd, env, root)
	if err != nil {
		return nil, nil, err
	}
	pre, _ := cmd.Flags().GetBool("pre")
	byTag := map[string]catalog.Release{}
	var candidates []versions.Candidate
	for _, r := range c.Releases() {
		if _, err := assets.Select(r, env.Platform, assets.Variant(state.Active.Variant)); err != nil {
			continue
		}
		byTag[r.Tag] = r
		candidates = append(candidates, versions.Candidate{Tag: r.Tag, Prerelease: r.Prerelease})
	}
	newer, ok := versions.Newer(candidates, state.Active.Tag, pre)
	if !ok {
		return state.Active, nil, nil
	}
	release := byTag[newer.Tag]
	return state.Active, &release, nil
}

func runCheck(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		active, release, err := findUpdate(cmd, env, root)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		latest := active.Tag
		if release != nil {
			latest = release.Tag
		}
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			if err := writeJSON(out, map[string]any{"current": active.Tag, "latest": latest, "updateAvailable": release != nil}); err != nil {
				return err
			}
		} else if release != nil {
			fmt.Fprintf(out, "新しい版があります: %s (いまは %s)。\"vmeltype update\" で更新します。\n", release.Tag, active.Tag)
		} else {
			fmt.Fprintf(out, "%s は最新です。\n", active.Tag)
		}
		if release != nil {
			return errUpdateAvailable
		}
		return nil
	}
}

func runUpdate(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		flags := cmd.Flags()
		dryRun, _ := flags.GetBool("dry-run")
		yes, _ := flags.GetBool("yes")
		asJSON, _ := flags.GetBool("json")
		overridePin, _ := flags.GetBool("override-pin")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		active, release, err := findUpdate(cmd, env, root)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if release == nil {
			if asJSON {
				return writeJSON(out, map[string]any{"changed": false, "active": active.StoreID})
			}
			fmt.Fprintf(out, "%s は最新です。\n", active.Tag)
			return nil
		}
		// 固定していれば、取得の手間をかける前に止める
		if state, err := lifecycle.LoadState(root); err != nil {
			return err
		} else if state.Pin != nil && !overridePin {
			return fmt.Errorf("%w (%s に固定しています)。%s に更新するには、\"vmeltype unpin\" で解除するか、--override-pin を付けてください", lifecycle.ErrPinned, state.Pin.Tag, release.Tag)
		}

		planOut := out
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		fmt.Fprintf(planOut, "%s を %s に更新します。\n", active.Tag, release.Tag)
		st := store.Store{Root: root}
		variant := assets.Variant(active.Variant)
		id := store.ID(release.Tag, env.Platform.OS, env.Platform.Arch, string(variant))
		target, err := st.Get(id)
		if err != nil {
			asset, err := assets.Select(*release, env.Platform, variant)
			if err != nil {
				return err
			}
			plan, err := newInstallPlan(st, id, *release, asset, false, false)
			if err != nil {
				return err
			}
			writeInstallPlan(planOut, plan)
			if dryRun {
				fmt.Fprintf(planOut, "取得したあと、%s に切り替えます。\n", release.Tag)
				if asJSON {
					return writeJSON(out, map[string]any{"dryRun": true, "from": active.Tag, "to": release.Tag, "install": plan})
				}
				return nil
			}
			confirmer := prompt.Confirmer{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr(), Interactive: env.Interactive && !asJSON, AssumeYes: yes}
			if err := confirmer.Confirm(fmt.Sprintf("%s を取得しますか？", release.Tag), prompt.DefaultYes); err != nil {
				return err
			}
			if target, err = downloadAndStore(cmd, env, st, plan, *release, asset); err != nil {
				return err
			}
		}
		return switchVersion(cmd, env, root, st, target, false, "%s に切り替えますか？")
	}
}
