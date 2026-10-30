package versions

import "testing"

func TestNewer(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		includePre bool
		want       string
	}{
		{name: "正式版の中で、いまより新しい最大を選ぶ", current: "v1.3.0", want: "v1.4.0"},
		{name: "プレリリースも含めれば、正式版と合わせた最大を選ぶ", current: "v1.3.0", includePre: true, want: "v2.0.0-pre.1"},
		{name: "いまが最新の正式版なら、新しい版はない", current: "v1.4.0", want: ""},
		{name: "いまがプレリリースで、正式版にそれより新しいものがなければ、下げない", current: "v1.4.1-pre.2", want: ""},
		{name: "いまのプレリリースの後に出た正式版は選ぶ", current: "v1.3.1-pre.3", want: "v1.4.0"},
		{name: "先頭の v がなくても比べる", current: "1.3.0", want: "v1.4.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Newer(candidates(sampleTags), tt.current, tt.includePre)

			if tt.want == "" {
				if ok {
					t.Errorf("新しい版はないはずが %s を選んだ", got.Tag)
				}
				return
			}
			if !ok || got.Tag != tt.want {
				t.Errorf("Newer = %q (%v), want %q", got.Tag, ok, tt.want)
			}
		})
	}
}
