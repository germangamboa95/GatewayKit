package config

import "testing"

func TestParseVar(t *testing.T) {
	cases := map[string]Var{
		"plain":            Literal("plain"),
		"v=$request_time":  Literal("v=$request_time"),
		"$literal:gateway": Literal("gateway"),
		"$literal:$foo":    Literal("$foo"),
		"$request_time":    {Kind: VarRequestTime},
		"$response_time":   {Kind: VarResponseTime},
		"$route_path":      {Kind: VarRoutePath},
		"$body":            {Kind: VarBody},
	}
	for in, want := range cases {
		got, err := ParseVar(in)
		if err != nil || got != want {
			t.Errorf("ParseVar(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	if _, err := ParseVar("$requst_time"); err == nil {
		t.Errorf("ParseVar accepted an unknown token")
	}
}

func TestTargetProbe(t *testing.T) {
	tg, err := NewTarget("http://localhost:3003", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !tg.Healthy() {
		t.Fatal("new target not healthy")
	}
	tg.RecordProbe(false, 3)
	tg.RecordProbe(false, 3)
	if !tg.Healthy() {
		t.Fatal("unhealthy before threshold")
	}
	if tg.RecordProbe(false, 3) {
		t.Fatal("healthy after threshold failures")
	}
	if !tg.RecordProbe(true, 3) || tg.Failures() != 0 {
		t.Fatal("one success did not recover the target")
	}
	if _, err := NewTarget("localhost:3003", 1); err == nil {
		t.Fatal("NewTarget accepted a URL without scheme and host")
	}
}
