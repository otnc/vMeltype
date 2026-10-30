package assets

import (
	"errors"
	"testing"

	"github.com/otnc/meltool/internal/catalog"
)

func release(tag string, names ...string) catalog.Release {
	r := catalog.Release{Tag: tag}
	for i, name := range names {
		r.Assets = append(r.Assets, catalog.Asset{ID: int64(i + 1), Name: name})
	}
	return r
}

var v140 = release("v1.4.0",
	"meltype-1.4.0-1.aarch64.rpm", "meltype-1.4.0-1.x86_64.rpm",
	"Meltype-1.4.0-linux-arm64.zip", "Meltype-1.4.0-linux.zip", "Meltype-1.4.0-mac.zip",
	"Meltype-1.4.0-setup.exe", "Meltype-1.4.0-windows-portable.zip", "Meltype-1.4.0-windows.zip",
	"meltype_1.4.0_amd64.deb", "meltype_1.4.0_arm64.deb",
)

func TestSelect(t *testing.T) {
	tests := []struct {
		name     string
		release  catalog.Release
		platform Platform
		variant  Variant
		want     string
		wantErr  error
	}{
		{name: "Windows x64 の通常版は windows.zip", release: v140, platform: Platform{"windows", "amd64"}, variant: Standard, want: "Meltype-1.4.0-windows.zip"},
		{name: "Windows x64 の portable 版は windows-portable.zip", release: v140, platform: Platform{"windows", "amd64"}, variant: Portable, want: "Meltype-1.4.0-windows-portable.zip"},
		{name: "macOS の Apple Silicon は mac.zip", release: v140, platform: Platform{"darwin", "arm64"}, variant: Standard, want: "Meltype-1.4.0-mac.zip"},
		{name: "Linux x86_64 は linux.zip", release: v140, platform: Platform{"linux", "amd64"}, variant: Standard, want: "Meltype-1.4.0-linux.zip"},
		{name: "Linux ARM64 は linux-arm64.zip", release: v140, platform: Platform{"linux", "arm64"}, variant: Standard, want: "Meltype-1.4.0-linux-arm64.zip"},
		{name: "プレリリースのタグも名前に含める", release: release("v1.3.1-pre.3", "Meltype-1.3.1-pre.3-windows.zip"), platform: Platform{"windows", "amd64"}, variant: Standard, want: "Meltype-1.3.1-pre.3-windows.zip"},
		{name: "portable 版のない古いバージョンでは選べない", release: release("v1.2.1", "Meltype-1.2.1-windows.zip"), platform: Platform{"windows", "amd64"}, variant: Portable, wantErr: ErrNoAsset},
		{name: "Linux ARM64 の配布物がない古いバージョンでは選べない", release: release("v1.2.1", "Meltype-1.2.1-linux.zip"), platform: Platform{"linux", "arm64"}, variant: Standard, wantErr: ErrNoAsset},
		{name: "v1.0.0 より前の形式は選ばない", release: release("v0.3.0", "Meltype-test-20261004-0601.zip", "Meltype-mac-test-20261004-0556.zip"), platform: Platform{"windows", "amd64"}, variant: Standard, wantErr: ErrNoAsset},
		{name: "macOS の Intel は未対応", release: v140, platform: Platform{"darwin", "amd64"}, variant: Standard, wantErr: ErrUnsupportedPlatform},
		{name: "Windows の ARM64 は未対応", release: v140, platform: Platform{"windows", "arm64"}, variant: Standard, wantErr: ErrUnsupportedPlatform},
		{name: "portable は Windows だけ", release: v140, platform: Platform{"linux", "amd64"}, variant: Portable, wantErr: ErrUnsupportedPlatform},
		{name: "同じ名前の配布物が複数あれば止める", release: release("v1.4.0", "Meltype-1.4.0-windows.zip", "Meltype-1.4.0-windows.zip"), platform: Platform{"windows", "amd64"}, variant: Standard, wantErr: ErrAmbiguousAsset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Select(tt.release, tt.platform, tt.variant)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got.Name != tt.want {
				t.Errorf("got %q, want %q", got.Name, tt.want)
			}
		})
	}
}
