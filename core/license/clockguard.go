package license

import (
	"log"
	"os"
	"strings"
	"time"
)

type clockGuard struct {
	clockFileName string
	buildTime     time.Time
	tolerance     time.Duration // slack for NTP sync, prevents false positives
}

// check returns the trusted time and whether the clock was tampered with
func (r *clockGuard) check(now time.Time) (time.Time, bool) {
	// find the minimum acceptable time
	lastSeen := r.readLastSeen()
	floor := lastSeen

	if r.buildTime.After(floor) {
		floor = r.buildTime
	}

	// detect a rolled-back clock
	if now.Before(floor.Add(-r.tolerance)) {
		return floor, true
	}

	// compute the new trusted time and save it
	trusted := now
	if lastSeen.After(trusted) {
		trusted = lastSeen
	}

	r.writeLastSeen(trusted)

	return trusted, false
}

func (r *clockGuard) readLastSeen() time.Time {
	clockFile, err := os.ReadFile(r.clockFileName)
	if err != nil {
		return r.buildTime
	}

	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(clockFile)))
	if err != nil {
		return r.buildTime
	}

	return t
}

func (r *clockGuard) writeLastSeen(t time.Time) {
	err := os.WriteFile(r.clockFileName, []byte(t.UTC().Format(time.RFC3339)), 0o600)
	if err != nil {
		log.Printf("[CLOCKGUARD] cannot write file %s", err.Error())
	}
}
