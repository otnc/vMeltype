package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	s, err := Load(t.TempDir())

	if err != nil || !s.StopAppOnYes || s.AutoSnapshotKeep != 10 {
		t.Errorf("設定がないときの既定が違う: %+v, %v", s, err)
	}
}

func TestSetCount(t *testing.T) {
	root := t.TempDir()

	if err := Set(root, "auto-snapshot-keep", "3"); err != nil {
		t.Fatal(err)
	}
	if s, err := Load(root); err != nil || s.AutoSnapshotKeep != 3 {
		t.Errorf("変えた値にならない: %+v, %v", s, err)
	}
	for _, value := range []string{"-1", "abc", "1.5", "true"} {
		if err := Set(root, "auto-snapshot-keep", value); err == nil {
			t.Errorf("auto-snapshot-keep = %s を受け付けた", value)
		}
	}
}

func TestSet(t *testing.T) {
	t.Run("変えた値を読み直せる", func(t *testing.T) {
		root := t.TempDir()

		if err := Set(root, "stop-app-on-yes", "false"); err != nil {
			t.Fatal(err)
		}

		if s, err := Load(root); err != nil || s.StopAppOnYes {
			t.Errorf("変えた値にならない: %+v, %v", s, err)
		}
	})
	t.Run("知らない項目を消さない", func(t *testing.T) {
		root := t.TempDir()
		os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"future-key": {"nested": 1}}`), 0o600)

		if err := Set(root, "stop-app-on-yes", "false"); err != nil {
			t.Fatal(err)
		}

		data, _ := os.ReadFile(filepath.Join(root, "config.json"))
		if !strings.Contains(string(data), "future-key") {
			t.Errorf("知らない項目が消えた: %s", data)
		}
	})
	t.Run("知らない項目と、型に合わない値は受け付けない", func(t *testing.T) {
		root := t.TempDir()
		for _, kv := range [][2]string{{"no-such-key", "true"}, {"stop-app-on-yes", "maybe"}} {
			if err := Set(root, kv[0], kv[1]); err == nil {
				t.Errorf("%s = %s を受け付けた", kv[0], kv[1])
			}
		}
		if _, err := os.Stat(filepath.Join(root, "config.json")); err == nil {
			t.Errorf("受け付けなかったのに設定を書いた")
		}
	})
}

func TestLoadRejectsBrokenFile(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"stop-app-on-yes": "yes"}`), 0o600)

	if _, err := Load(root); err == nil {
		t.Errorf("型の違う値を黙って既定にした")
	}
}

func TestItems(t *testing.T) {
	root := t.TempDir()
	Set(root, "stop-app-on-yes", "false")

	items, err := Items(root)

	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Item{}
	for _, item := range items {
		byKey[item.Key] = item
	}
	if got := byKey["stop-app-on-yes"]; got.Value != "false" || got.Default != "true" || got.Description == "" {
		t.Errorf("項目が違う: %+v", got)
	}
	if got := byKey["auto-snapshot-keep"]; got.Value != "10" || got.Default != "10" || got.Description == "" {
		t.Errorf("項目が違う: %+v", got)
	}
}
