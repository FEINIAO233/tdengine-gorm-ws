package interp

import (
	"errors"
	"strconv"

	"github.com/FEINIAO233/tdengine-gorm-ws/clause/window"
	"gorm.io/gorm/clause"
)

// Range represents RANGE(start[, end]) for an INTERP query.
type Range struct {
	start interface{}
	end   interface{}
}

func SetRange(start interface{}, end ...interface{}) Range {
	result := Range{start: start}
	if len(end) > 0 {
		result.end = end[0]
	}
	return result
}

func (Range) Name() string { return "RANGE" }

func (value Range) Build(builder clause.Builder) {
	if value.start == nil {
		builder.AddError(errors.New("tdengine: interpolation range requires a start value"))
		return
	}
	builder.WriteString("RANGE(")
	builder.AddVar(builder, value.start)
	if value.end != nil {
		builder.WriteByte(',')
		builder.AddVar(builder, value.end)
	}
	builder.WriteByte(')')
}

func (value Range) MergeClause(target *clause.Clause) {
	target.Name = ""
	target.Expression = value
}

// Every represents EVERY(duration) for an INTERP query.
type Every struct {
	duration window.Duration
}

func SetEvery(duration window.Duration) Every { return Every{duration: duration} }

func (Every) Name() string { return "EVERY" }

func (value Every) Build(builder clause.Builder) {
	if value.duration.Value == 0 {
		builder.AddError(errors.New("tdengine: interpolation interval must be positive"))
		return
	}
	builder.WriteString("EVERY(")
	builder.WriteString(strconv.FormatUint(value.duration.Value, 10))
	builder.WriteString(string(value.duration.Unit))
	builder.WriteByte(')')
}

func (value Every) MergeClause(target *clause.Clause) {
	target.Name = ""
	target.Expression = value
}
