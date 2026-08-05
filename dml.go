package tdengine_gorm

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	// ErrUpdateNotSupported explains TDengine's insert-based update semantics.
	ErrUpdateNotSupported = errors.New("tdengine: GORM UPDATE is not supported; insert the same timestamp again to update a row")
	// ErrDeleteNotSupported directs callers to the time-range API.
	ErrDeleteNotSupported = errors.New("tdengine: GORM DELETE is not supported; use DeleteTimeRange")
	// ErrDeleteRangeRequired prevents an unbounded, irreversible deletion.
	ErrDeleteRangeRequired = errors.New("tdengine: at least one delete time bound is required")
)

// Update blocks SQL UPDATE statements. TDengine updates time-series rows by
// inserting the same timestamp again.
func (dialect Dialect) Update(db *gorm.DB) {
	db.AddError(ErrUpdateNotSupported)
}

// Delete blocks generic GORM deletes because TDengine only permits predicates
// on the first timestamp column.
func (dialect Dialect) Delete(db *gorm.DB) {
	db.AddError(ErrDeleteNotSupported)
}

// DeleteTimeRange deletes rows using TDengine's _rowts pseudocolumn. Start is
// inclusive and end is exclusive. A nil bound leaves that side open; at least
// one bound is required.
func DeleteTimeRange(db *gorm.DB, table string, start, end *time.Time) *gorm.DB {
	tx := db.Session(&gorm.Session{})
	if start == nil && end == nil {
		tx.AddError(ErrDeleteRangeRequired)
		return tx
	}
	if start != nil && end != nil && !start.Before(*end) {
		tx.AddError(errors.New("tdengine: delete range start must be before end"))
		return tx
	}

	var statement strings.Builder
	statement.WriteString("DELETE FROM ")
	tx.Dialector.QuoteTo(&statement, table)
	statement.WriteString(" WHERE ")
	args := make([]interface{}, 0, 2)
	if start != nil {
		statement.WriteString("_rowts >= ?")
		args = append(args, *start)
	}
	if start != nil && end != nil {
		statement.WriteString(" AND ")
	}
	if end != nil {
		statement.WriteString("_rowts < ?")
		args = append(args, *end)
	}
	return tx.Exec(statement.String(), args...)
}
