package paperboy

import (
	"strconv"
	"time"
)

// idleMA is the current below which the fuel gauge counts as idle: it reports
// in 10 mA steps, so a few milliamps are noise, not charging.
const idleMA = 20

// copyFloat detaches a reading from the sync request, so the stored state never
// shares a pointer with it.
func copyFloat(p *float64) *float64 {
	if p == nil {
		return nil
	}
	value := *p
	return &value
}

// chargeReading is the battery state at one end of a charging episode: the
// state of charge when the hardware has a fuel gauge, else the voltage.
type chargeReading struct {
	Percent *float64 `json:"percent,omitempty"`
	Volts   string   `json:"volts,omitempty"`
}

// chargeEpisode is one stretch of charging (issue #205): Start is the sync that
// saw the gauge current above the idle threshold, End the first one below it.
// A running episode has no End yet; the last finished one stays in the state.
type chargeEpisode struct {
	Start time.Time      `json:"start"`
	From  *chargeReading `json:"from,omitempty"`
	End   time.Time      `json:"end,omitempty"`
	To    *chargeReading `json:"to,omitempty"`
}

// charging reports whether a sync's battery reading shows the cell taking
// charge: the fuel gauge current above the idle threshold, the same rule the
// card's power row uses. Hardware without a gauge reports no episodes.
func charging(b *syncBattery) bool {
	return b != nil && b.MA != nil && *b.MA > idleMA
}

// trackCharge folds one sync's battery reading into the state: it opens an
// episode when charging starts and closes it with the next reading below the
// threshold, keeping the charge and voltage of both ends. An episode that ends
// while Paperboy is down closes at the next sync, so its age is approximate.
func trackCharge(st *deviceState, b *syncBattery, now time.Time) {
	if b == nil {
		return
	}
	reading := chargeReading{Percent: copyFloat(b.Percent), Volts: strconv.FormatFloat(b.Volts, 'f', 2, 64)}
	switch {
	case charging(b) && st.Charge == nil:
		st.Charge = &chargeEpisode{Start: now, From: &reading}
	case !charging(b) && st.Charge != nil:
		episode := *st.Charge
		episode.End, episode.To = now, &reading
		st.LastCharge, st.Charge = &episode, nil
	}
}

// trackBattery folds one sync's battery reading into the state: the charging
// episode (issue #205) and the daily samples of the runtime estimate (issue
// #207). A charge is no discharge, so it clears the samples and the series
// starts again with the first reading after it.
func trackBattery(st *deviceState, b *syncBattery, now time.Time) {
	if b == nil {
		return
	}
	trackCharge(st, b, now)
	if charging(b) {
		st.BatteryHistory = nil
		return
	}
	st.BatteryHistory = addSample(st.BatteryHistory, b, now)
}
