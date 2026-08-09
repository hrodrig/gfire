package job

import "testing"

func TestParseRecurringCron_sixFieldAndDescriptor(t *testing.T) {
	if _, err := ParseRecurringCron("0 0 2 * * *"); err != nil {
		t.Fatalf("6-field: %v", err)
	}
	if _, err := ParseRecurringCron("@every 1h"); err != nil {
		t.Fatalf("descriptor: %v", err)
	}
	// Classic 5-field crontab must fail (engine uses WithSeconds).
	if _, err := ParseRecurringCron("0 2 * * *"); err == nil {
		t.Fatal("expected 5-field expression to fail")
	}
}
