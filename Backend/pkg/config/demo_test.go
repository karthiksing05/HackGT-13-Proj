package config

import (
	"strings"
	"testing"
)

func TestDemoDate(t *testing.T) {
	c, err := Parse(env(map[string]string{"APP_ENV": "dev"}))
	if err != nil || c.DemoDate != "" || c.DemoClock() != nil {
		t.Fatalf("unset DEMO_DATE must mean real time: %v %+v", err, c)
	}
	c, err = Parse(env(map[string]string{"APP_ENV": "dev", "DEMO_DATE": "2026-09-24"}))
	if err != nil || c.DemoClock() == nil || c.DemoClock().Date() != "2026-09-24" || c.DemoClock().Location().String() != "America/New_York" {
		t.Fatalf("DEMO_DATE=2026-09-24: %v %+v", err, c)
	}
	for _, bad := range []string{"2026-9-24", "Sep 24", "2026-09-31"} {
		if _, err := Parse(env(map[string]string{"APP_ENV": "dev", "DEMO_DATE": bad})); err == nil || !strings.Contains(err.Error(), "DEMO_DATE") {
			t.Errorf("DEMO_DATE=%q accepted: %v", bad, err)
		}
	}
	if _, err := Parse(env(map[string]string{"APP_ENV": "dev", "DEMO_DATE": "2026-09-24", "DEMO_TZ": "Nowhere/Harbor"})); err == nil || !strings.Contains(err.Error(), "DEMO_TZ") {
		t.Errorf("a bad DEMO_TZ with DEMO_DATE accepted: %v", err)
	}
	if _, err := Parse(env(map[string]string{"APP_ENV": "dev", "DEMO_TZ": "Nowhere/Harbor"})); err != nil {
		t.Errorf("without DEMO_DATE, DEMO_TZ is only the seed's concern: %v", err)
	}
}
