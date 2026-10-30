package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAppConfig(t *testing.T) {
	t.Run("set で変えた値を show で表示する", func(t *testing.T) {
		l := newLocalEnv(t)

		set := l.run(t, false, "", "app", "config", "set", "stop-app-on-yes", "false")
		show := l.run(t, false, "", "app", "config", "show", "--json")

		if set.code != ExitOK {
			t.Fatalf("code = %d, stderr = %s", set.code, set.stderr)
		}
		var got struct {
			Settings []struct{ Key, Value string } `json:"settings"`
		}
		json.Unmarshal([]byte(show.stdout), &got)
		if len(got.Settings) != 1 || got.Settings[0].Key != "stop-app-on-yes" || got.Settings[0].Value != "false" {
			t.Errorf("show が変えた値を示していない: %s", show.stdout)
		}
	})
	t.Run("show は既定の値と説明も表示する", func(t *testing.T) {
		got := newLocalEnv(t).run(t, false, "", "app", "config", "show")

		if got.code != ExitOK || !strings.Contains(got.stdout, "stop-app-on-yes") || !strings.Contains(got.stdout, "true") {
			t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
	})
	t.Run("知らない項目は使い方の誤りにする", func(t *testing.T) {
		got := newLocalEnv(t).run(t, false, "", "app", "config", "set", "no-such-key", "true")

		if got.code != ExitUsage || !strings.Contains(got.stderr, "stop-app-on-yes") {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
}
