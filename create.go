package tdengine_gorm

import (
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
)

func (dialector *Dialect) Create(db *gorm.DB) {
	if db.Error != nil {
		return
	}
	if db.Statement.Schema != nil && !db.Statement.Unscoped {
		for _, c := range db.Statement.Schema.CreateClauses {
			db.Statement.AddClause(c)
		}
	}

	if db.Statement.SQL.String() == "" {
		db.Statement.SQL.Grow(180)
		db.Statement.AddClauseIfNotExists(clause.Insert{})
		db.Statement.AddClause(callbacks.ConvertToCreateValues(db.Statement))

		db.Statement.Build(db.Statement.BuildClauses...)
	}

	if db.DryRun || db.Error != nil {
		return
	}

	result, err := db.Statement.ConnPool.ExecContext(
		db.Statement.Context, db.Statement.SQL.String(), db.Statement.Vars...,
	)
	if err != nil {
		db.AddError(err)
		return
	}

	db.RowsAffected, _ = result.RowsAffected()
	if db.RowsAffected == 0 {
		return
	}
}
