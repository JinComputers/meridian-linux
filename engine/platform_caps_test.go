package engine

import "testing"

func TestClientPlatformCaps(t *testing.T) {
	for _, tc := range []struct{ in, platform, want string }{
		{"", "android", "platform=android"},
		{"echo,multi,gen=7", "ios", "echo,multi,gen=7,platform=ios"},
		{"echo,platform=bogus", "darwin", "echo,platform=macos"},
		{"echo", "linux", "echo,platform=linux"},
		{"echo", "windows", "echo,platform=windows"},
		{"echo,platform=bogus", "unsupported", "echo"},
	} {
		if got := capsWithPlatform(tc.in, tc.platform); got != tc.want {
			t.Errorf("capsWithPlatform(%q,%q) = %q, want %q", tc.in, tc.platform, got, tc.want)
		}
	}
}
