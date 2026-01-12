package batch

import (
	"testing"
	"time"
)

func TestParseCron(t *testing.T) {
	tests := []struct {
		expr    string
		wantErr bool
	}{
		{"0 22 * * *", false},   // 10 PM daily
		{"0 12 * * 1-5", false}, // noon weekdays
		{"*/5 * * * *", false},  // every 5 minutes
		{"invalid", true},
	}

	for _, tt := range tests {
		_, err := ParseCron(tt.expr)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseCron(%q) error = %v, wantErr %v", tt.expr, err, tt.wantErr)
		}
	}
}

func TestBatchConfig_Validate(t *testing.T) {
	cfg := BatchConfig{
		Name:        "overnight",
		Cron:        "0 22 * * *",
		MaxTasks:    10,
		MaxDuration: 8 * time.Hour,
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid config should not error: %v", err)
	}

	cfg.Name = ""
	if err := cfg.Validate(); err == nil {
		t.Error("Empty name should error")
	}
}

func TestBatchScheduler_NextRun(t *testing.T) {
	cfg := BatchConfig{
		Name:     "test",
		Cron:     "0 22 * * *", // 10 PM daily
		MaxTasks: 5,
	}

	sched, err := NewScheduler([]BatchConfig{cfg})
	if err != nil {
		t.Fatal(err)
	}

	next := sched.NextRun("test")
	if next.IsZero() {
		t.Error("NextRun should return a time")
	}

	// Should be in the future
	if !next.After(time.Now()) {
		t.Error("NextRun should be in the future")
	}
}

func TestBatchScheduler_ShouldRun(t *testing.T) {
	cfg := BatchConfig{
		Name:        "test",
		Cron:        "* * * * *", // Every minute
		MaxTasks:    5,
		MaxDuration: time.Hour,
	}

	sched, err := NewScheduler([]BatchConfig{cfg})
	if err != nil {
		t.Fatal(err)
	}

	// Mark as last run a minute ago
	sched.lastRun["test"] = time.Now().Add(-2 * time.Minute)

	if !sched.ShouldRun("test") {
		t.Error("Should run after cron interval passed")
	}
}
