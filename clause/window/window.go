package window

import (
	"errors"
	"strconv"

	"gorm.io/gorm/clause"
)

const (
	SESSION = iota + 1
	STATE
	INTERVAL
	EVENT
	COUNT
)

//[SESSION(ts_col, tol_val)]
//[STATE_WINDOW(col)]
//[INTERVAL(interval_val [, interval_offset]) [SLIDING sliding_val]]

type Window struct {
	windowType  int
	tsColumn    string
	stateColumn string
	duration    *Duration
	offset      *Duration
	sliding     *Duration
	start       clause.Expression
	end         clause.Expression
	count       uint64
	countSlide  uint64
	columns     []string
}

// SetEventWindow creates EVENT_WINDOW START WITH ... END WITH .... Use
// clause.Expr for conditions so values remain parameterized.
func SetEventWindow(start, end clause.Expression) Window {
	return Window{windowType: EVENT, start: start, end: end}
}

// SetCountWindow creates COUNT_WINDOW(count[, columns...]). The count must be
// at least 2. Column filtering is supported by TDengine 3.3.7 and later.
func SetCountWindow(count uint64, columns ...string) Window {
	return Window{windowType: COUNT, count: count, columns: columns}
}

// SetCountSliding sets the row sliding amount for a count window.
func (sc Window) SetCountSliding(sliding uint64) Window {
	if sc.windowType == COUNT {
		sc.countSlide = sliding
	}
	return sc
}

// SetSessionWindow create a session window [SESSION(ts_col, tol_val)]
func SetSessionWindow(tsColumn string, duration Duration) Window {
	return Window{windowType: SESSION, tsColumn: tsColumn, duration: &duration}
}

// SetStateWindow create a state window [STATE_WINDOW(col)]
func SetStateWindow(column string) Window {
	return Window{windowType: STATE, stateColumn: column}
}

// SetInterval create an interval window [INTERVAL(interval_val [, interval_offset]) [SLIDING sliding_val]]
func SetInterval(duration Duration) Window {
	return Window{windowType: INTERVAL, duration: &duration}
}

// SetOffset set offset to interval window
func (sc Window) SetOffset(offset Duration) Window {
	if sc.windowType == INTERVAL {
		sc.offset = &offset
	}
	return sc
}

// SetSliding set sliding to interval window
func (sc Window) SetSliding(sliding Duration) Window {
	if sc.windowType == INTERVAL {
		sc.sliding = &sliding
	}
	return sc
}

func (sc Window) Build(builder clause.Builder) {
	switch sc.windowType {
	case SESSION:
		builder.WriteString("SESSION(")
		builder.WriteQuoted(clause.Column{Name: sc.tsColumn})
		builder.WriteByte(',')
		builder.WriteString(strconv.FormatUint(sc.duration.Value, 10))
		builder.WriteString(string(sc.duration.Unit))
		builder.WriteByte(')')
	case STATE:
		builder.WriteString("STATE_WINDOW(")
		builder.WriteQuoted(clause.Column{Name: sc.stateColumn})
		builder.WriteByte(')')
	case INTERVAL:
		builder.WriteString("INTERVAL(")
		builder.WriteString(strconv.FormatUint(sc.duration.Value, 10))
		builder.WriteString(string(sc.duration.Unit))
		if sc.offset != nil {
			builder.WriteByte(',')
			builder.WriteString(strconv.FormatUint(sc.offset.Value, 10))
			builder.WriteString(string(sc.offset.Unit))
		}
		builder.WriteByte(')')
		if sc.sliding != nil {
			builder.WriteString(" SLIDING(")
			builder.WriteString(strconv.FormatUint(sc.sliding.Value, 10))
			builder.WriteString(string(sc.sliding.Unit))
			builder.WriteByte(')')
		}
	case EVENT:
		if sc.start == nil || sc.end == nil {
			builder.AddError(errors.New("tdengine: event window requires start and end conditions"))
			return
		}
		builder.WriteString("EVENT_WINDOW START WITH ")
		sc.start.Build(builder)
		builder.WriteString(" END WITH ")
		sc.end.Build(builder)
	case COUNT:
		if sc.count < 2 {
			builder.AddError(errors.New("tdengine: count window size must be at least 2"))
			return
		}
		if sc.countSlide > sc.count {
			builder.AddError(errors.New("tdengine: count window sliding must not exceed its size"))
			return
		}
		builder.WriteString("COUNT_WINDOW(")
		builder.WriteString(strconv.FormatUint(sc.count, 10))
		if sc.countSlide > 0 {
			builder.WriteByte(',')
			builder.WriteString(strconv.FormatUint(sc.countSlide, 10))
		}
		for _, column := range sc.columns {
			builder.WriteByte(',')
			builder.WriteQuoted(clause.Column{Name: column})
		}
		builder.WriteByte(')')
	}
}

func (sc Window) Name() string {
	return "WINDOW"
}

func (sc Window) MergeClause(c *clause.Clause) {
	c.Name = ""
	c.Expression = sc
}
