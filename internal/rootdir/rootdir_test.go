package rootdir

import (
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	home := filepath.FromSlash("/home/alice")
	tests := []struct {
		name    string
		goos    string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{name: "Windows は LOCALAPPDATA の下", goos: "windows", env: map[string]string{"LOCALAPPDATA": `C:\Users\alice\AppData\Local`}, want: filepath.Join(`C:\Users\alice\AppData\Local`, "Meltool")},
		{name: "Windows で LOCALAPPDATA がなければ決めない", goos: "windows", wantErr: true},
		{name: "macOS は Application Support の下", goos: "darwin", want: filepath.Join(home, "Library", "Application Support", "Meltool")},
		{name: "Linux は XDG_DATA_HOME の下", goos: "linux", env: map[string]string{"XDG_DATA_HOME": "/data"}, want: filepath.Join("/data", "meltool")},
		{name: "Linux で XDG_DATA_HOME がなければ ~/.local/share の下", goos: "linux", want: filepath.Join(home, ".local", "share", "meltool")},
		{name: "XDG_DATA_HOME が相対パスなら無視する", goos: "linux", env: map[string]string{"XDG_DATA_HOME": "data"}, want: filepath.Join(home, ".local", "share", "meltool")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }

			got, err := Default(tt.goos, getenv, home)

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
