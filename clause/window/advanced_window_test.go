package window_test

import (
	"testing"

	"github.com/FEINIAO233/tdengine-gorm-ws/clause/tests"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/window"
	"gorm.io/gorm/clause"
)

func TestEventWindow(t *testing.T) {
	tests.CheckBuildClauses(t, []clause.Interface{
		window.SetEventWindow(
			clause.Expr{SQL: "voltage >= ?", Vars: []interface{}{220}},
			clause.Expr{SQL: "voltage < ?", Vars: []interface{}{220}},
		),
	}, []string{"EVENT_WINDOW START WITH voltage >= ? END WITH voltage < ?"}, [][][]interface{}{{{220, 220}}})
}

func TestCountWindow(t *testing.T) {
	tests.CheckBuildClauses(t, []clause.Interface{
		window.SetCountWindow(10, "voltage").SetCountSliding(5),
	}, []string{"COUNT_WINDOW(10,5,voltage)"}, nil)
}
