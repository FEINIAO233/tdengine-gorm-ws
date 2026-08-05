package interp_test

import (
	"testing"
	"time"

	"github.com/FEINIAO233/tdengine-gorm-ws/clause/interp"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/tests"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/window"
	"gorm.io/gorm/clause"
)

func TestInterpolationClauses(t *testing.T) {
	start := time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	tests.CheckBuildClauses(t, []clause.Interface{
		interp.SetRange(start, end),
		interp.SetEvery(window.Duration{Value: 1, Unit: window.Minute}),
	}, []string{"RANGE(?,?) EVERY(1m)"}, [][][]interface{}{{{start, end}}})
}
