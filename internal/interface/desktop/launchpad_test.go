package desktop

import (
	"reflect"
	"testing"
)

func TestLaunchArgv(t *testing.T) {
	cases := []struct {
		name     string
		goos     string
		target   string
		args     []string
		wantName string
		wantArgv []string
	}{
		{"darwin app no args", "darwin", "/Applications/Code.app", nil, "open", []string{"/Applications/Code.app"}},
		{"darwin app with args", "darwin", "/Applications/Code.app", []string{"--new-window"}, "open", []string{"-a", "/Applications/Code.app", "--args", "--new-window"}},
		{"windows", "windows", `C:\Program Files\App\app.exe`, nil, "cmd", []string{"/c", "start", "", `C:\Program Files\App\app.exe`}},
		{"linux no args", "linux", "/usr/bin/xterm", nil, "xdg-open", []string{"/usr/bin/xterm"}},
		{"linux with args runs directly", "linux", "/usr/bin/xterm", []string{"-e", "htop"}, "/usr/bin/xterm", []string{"-e", "htop"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, argv := launchArgv(c.goos, c.target, c.args)
			if name != c.wantName || !reflect.DeepEqual(argv, c.wantArgv) {
				t.Fatalf("launchArgv = (%q, %v), want (%q, %v)", name, argv, c.wantName, c.wantArgv)
			}
		})
	}
}

func TestDeriveLaunchpadName(t *testing.T) {
	cases := map[string]string{
		"/Applications/Visual Studio Code.app": "Visual Studio Code",
		"/usr/bin/htop":                        "htop",
		"htop":                                 "htop",
		"https://github.com/icloudbb/buildmax": "github.com",
		"https://www.example.com":              "example.com",
	}
	for target, want := range cases {
		if got := deriveLaunchpadName(target); got != want {
			t.Errorf("deriveLaunchpadName(%q) = %q, want %q", target, got, want)
		}
	}
}
