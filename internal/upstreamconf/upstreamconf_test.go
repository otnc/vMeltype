package upstreamconf

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDisableAutoUpdate(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		want     string
		original Original
	}{
		{
			name:     "true を false にし、ほかの項目と並びと空白を保つ",
			body:     "{\n  \"Enabled\": true,\n  \"AutoUpdate\": true,\n  \"Future\": {\"AutoUpdate\": true}\n}",
			want:     "{\n  \"Enabled\": true,\n  \"AutoUpdate\": false,\n  \"Future\": {\"AutoUpdate\": true}\n}",
			original: Original{FileExisted: true, AutoUpdate: ptr(true)},
		},
		{
			name:     "項目がなければ先頭に足す",
			body:     "{\n  \"Enabled\": true\n}",
			want:     "{\"AutoUpdate\": false,\n  \"Enabled\": true\n}",
			original: Original{FileExisted: true},
		},
		{
			name:     "空のオブジェクトにも足す",
			body:     "{}",
			want:     "{\"AutoUpdate\": false}",
			original: Original{FileExisted: true},
		},
		{
			name:     "既に false なら変えない",
			body:     "{\"AutoUpdate\":false}",
			want:     "{\"AutoUpdate\":false}",
			original: Original{FileExisted: true, AutoUpdate: ptr(false)},
		},
		{
			name:     "BOM を保つ",
			body:     "\xEF\xBB\xBF{\"AutoUpdate\": true}",
			want:     "\xEF\xBB\xBF{\"AutoUpdate\": false}",
			original: Original{FileExisted: true, AutoUpdate: ptr(true)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.body)

			original, err := DisableAutoUpdate(path)

			if err != nil {
				t.Fatal(err)
			}
			if got := read(t, path); got != tt.want {
				t.Errorf("中身 = %q, want %q", got, tt.want)
			}
			if original.FileExisted != tt.original.FileExisted || !sameBool(original.AutoUpdate, tt.original.AutoUpdate) {
				t.Errorf("元の値 = %+v, want %+v", original, tt.original)
			}
		})
	}

	t.Run("ファイルがなければ、自動更新を止めた設定を作る", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")

		original, err := DisableAutoUpdate(path)

		if err != nil || original.FileExisted || read(t, path) != "{\"AutoUpdate\": false}" {
			t.Errorf("original = %+v, err = %v", original, err)
		}
	})
}

func TestDisableAutoUpdateLeavesUnreadableConfig(t *testing.T) {
	for name, body := range map[string]string{
		"壊れた JSON":       "{\"AutoUpdate\": tru",
		"真偽値でない値":        "{\"AutoUpdate\": \"yes\"}",
		"オブジェクトでない JSON": "[true]",
		"後ろに余計なもの":       "{\"AutoUpdate\": true} {}",
	} {
		t.Run(name+"は変えずにエラーにする", func(t *testing.T) {
			path := writeConfig(t, body)

			_, err := DisableAutoUpdate(path)

			if err == nil || read(t, path) != body {
				t.Errorf("err = %v, 中身 = %q", err, read(t, path))
			}
		})
	}
}

func ptr(b bool) *bool { return &b }

func sameBool(a, b *bool) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
