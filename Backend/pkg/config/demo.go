package config

import (
	"Backend/pkg/democlock"
	"strings"
)

// validateDemoDate refuses a DEMO_DATE that is not a YYYY-MM-DD date (or a
// DEMO_TZ that is not a zone while it is set). Unset, every account lives in
// real time and DEMO_TZ is only the seed's zone.
func (c *Config) validateDemoDate() error {
	if strings.TrimSpace(c.DemoDate) == "" {
		return nil
	}
	_, err := democlock.New(c.DemoDate, c.DemoTZ)
	return err
}

// DemoClock is DEMO_DATE's clock (pkg/democlock); nil when it is unset.
// Validate has already refused a bad value.
func (c *Config) DemoClock() *democlock.Clock {
	if strings.TrimSpace(c.DemoDate) == "" {
		return nil
	}
	clock, err := democlock.New(c.DemoDate, c.DemoTZ)
	if err != nil {
		return nil
	}
	return clock
}
