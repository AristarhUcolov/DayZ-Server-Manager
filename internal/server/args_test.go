package server

import (
	"reflect"
	"strings"
	"testing"

	"dayzmanager/internal/config"
)

func TestSplitParams(t *testing.T) {
	cases := map[string][]string{
		"":                                   nil,
		"   ":                                nil,
		"-a -b":                              {"-a", "-b"},
		`-profiles="C:\My Profiles" -x`:      {`-profiles=C:\My Profiles`, "-x"},
		"-scrAllowFileWrite\t-noPause\r\n-z": {"-scrAllowFileWrite", "-noPause", "-z"},
		`""`:                                 {""},
	}
	for in, want := range cases {
		if got := SplitParams(in); !reflect.DeepEqual(got, want) {
			t.Errorf("SplitParams(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildArgsLimitFPSAndExtra(t *testing.T) {
	c := &Controller{serverDir: `C:\srv`}
	c.SetScheduleConfig(&config.Manager{ServerCfg: "serverDZ.cfg", ServerPort: 2302, CPUCount: 4,
		LimitFPS: 120, ExtraLaunchParams: "-scrAllowFileWrite"})
	args := strings.Join(c.buildArgs(), " ")
	if !strings.Contains(args, "-limitFPS=120") {
		t.Fatalf("missing -limitFPS: %s", args)
	}
	if !strings.HasSuffix(args, "-scrAllowFileWrite") {
		t.Fatalf("extra params not appended last: %s", args)
	}
	c.SetScheduleConfig(&config.Manager{ServerCfg: "serverDZ.cfg", ServerPort: 2302, CPUCount: 4})
	if args := strings.Join(c.buildArgs(), " "); strings.Contains(args, "limitFPS") {
		t.Fatalf("-limitFPS passed while 0: %s", args)
	}
}
