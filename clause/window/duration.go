package window

import (
	"errors"
	"strconv"
	"time"
)

type UnitType string

// b(ns), u(us), a(ms), s, m, h, d, w, n(month), q(quarter), y(year).
const (
	Nanosecond  UnitType = "b"
	Microsecond UnitType = "u"
	Millisecond UnitType = "a"
	Second      UnitType = "s"
	Minute      UnitType = "m"
	Hour        UnitType = "h"
	Day         UnitType = "d"
	Week        UnitType = "w"
	Month       UnitType = "n"
	Quarter     UnitType = "q"
	Year        UnitType = "y"
)

var durationMap = map[UnitType]struct{}{
	Nanosecond:  {},
	Microsecond: {},
	Millisecond: {},
	Second:      {},
	Minute:      {},
	Hour:        {},
	Day:         {},
	Week:        {},
	Month:       {},
	Quarter:     {},
	Year:        {},
}

type Duration struct {
	Value uint64
	Unit  UnitType
}

func NewDurationFromTimeDuration(duration time.Duration) (*Duration, error) {
	if duration <= 0 {
		return nil, errors.New("duration does not allow negative numbers")
	}
	if duration%time.Microsecond == 0 {
		return &Duration{Value: uint64(duration.Microseconds()), Unit: Microsecond}, nil
	}
	return &Duration{Value: uint64(duration.Nanoseconds()), Unit: Nanosecond}, nil
}

func ParseDuration(durationString string) (*Duration, error) {
	if len(durationString) < 2 {
		return nil, errors.New("parse duration error")
	}
	unit := UnitType(durationString[len(durationString)-1:])
	_, valid := durationMap[unit]
	if !valid {
		return nil, errors.New("unit not valid")
	}
	value := durationString[:len(durationString)-1]
	v, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return nil, err
	}
	return &Duration{
		Value: v,
		Unit:  unit,
	}, nil
}
