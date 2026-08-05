package tdengine_gorm

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

var (
	ErrTagIndexOnly       = errors.New("tdengine: indexes are supported only on supertable tags")
	ErrSingleTagIndexOnly = errors.New("tdengine: a tag index must contain exactly one tag")
)

type tdIndex struct {
	table  string
	name   string
	column string
	option string
}

func (index tdIndex) Table() string            { return index.table }
func (index tdIndex) Name() string             { return index.name }
func (index tdIndex) Columns() []string        { return []string{index.column} }
func (index tdIndex) PrimaryKey() (bool, bool) { return false, true }
func (index tdIndex) Unique() (bool, bool)     { return false, true }
func (index tdIndex) Option() string           { return index.option }

type indexMetadata struct {
	TableName  string `gorm:"column:table_name"`
	IndexName  string `gorm:"column:index_name"`
	ColumnName string `gorm:"column:column_name"`
}

func (m Migrator) CreateIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		index, err := m.tagIndex(stmt, name)
		if err != nil {
			return err
		}
		stable, err := m.isStable(stmt.Table)
		if err != nil {
			return err
		}
		if !stable {
			return ErrTagIndexOnly
		}
		return m.DB.Exec(
			"CREATE INDEX ? ON ? (?)",
			clause.Column{Name: index.Name}, clause.Table{Name: stmt.Table}, clause.Column{Name: index.Fields[0].DBName},
		).Error
	})
}

func (m Migrator) DropIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		column := ""
		if stmt.Schema != nil {
			if index := stmt.Schema.LookIndex(name); index != nil {
				name = index.Name
				if len(index.Fields) == 1 {
					column = index.Fields[0].DBName
				}
			}
		}
		if column != "" {
			var actualName string
			if err := m.DB.Raw(
				"SELECT index_name FROM information_schema.ins_indexes WHERE db_name = ? AND table_name = ? AND (index_name = ? OR column_name = ?) LIMIT 1",
				m.CurrentDatabase(), stmt.Table, name, column,
			).Scan(&actualName).Error; err != nil {
				return err
			}
			if actualName != "" {
				name = actualName
			}
		}
		return m.DB.Exec("DROP INDEX ?", clause.Column{Name: name}).Error
	})
}

func (m Migrator) HasIndex(value interface{}, name string) bool {
	found := false
	_ = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		column := ""
		if stmt.Schema != nil {
			if index := stmt.Schema.LookIndex(name); index != nil {
				name = index.Name
				if len(index.Fields) == 1 {
					column = index.Fields[0].DBName
				}
			}
		}
		var count int64
		query := "SELECT count(*) FROM information_schema.ins_indexes WHERE db_name = ? AND table_name = ? AND index_name = ?"
		args := []interface{}{m.CurrentDatabase(), stmt.Table, name}
		if column != "" {
			query = "SELECT count(*) FROM information_schema.ins_indexes WHERE db_name = ? AND table_name = ? AND (index_name = ? OR column_name = ?)"
			args = append(args, column)
		}
		if err := m.DB.Raw(query, args...).Scan(&count).Error; err != nil {
			return err
		}
		found = count > 0
		return nil
	})
	return found
}

func (m Migrator) GetIndexes(value interface{}) (indexes []gorm.Index, err error) {
	err = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		var metadata []indexMetadata
		if queryErr := m.DB.Raw(
			"SELECT table_name, index_name, column_name FROM information_schema.ins_indexes WHERE db_name = ? AND table_name = ? ORDER BY index_name",
			m.CurrentDatabase(), stmt.Table,
		).Scan(&metadata).Error; queryErr != nil {
			return queryErr
		}
		for _, item := range metadata {
			indexes = append(indexes, tdIndex{table: item.TableName, name: item.IndexName, column: item.ColumnName})
		}
		return nil
	})
	return indexes, err
}

func (m Migrator) createMissingIndexes(value interface{}) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema == nil {
			return errors.New("tdengine: failed to parse migration schema")
		}
		for _, index := range stmt.Schema.ParseIndexes() {
			if _, err := m.tagIndex(stmt, index.Name); err != nil {
				return err
			}
			exists, err := m.hasTagIndex(stmt.Table, index.Name, index.Fields[0].DBName)
			if err != nil {
				return err
			}
			if !exists {
				if err := m.CreateIndex(value, index.Name); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (m Migrator) hasTagIndex(table, name, column string) (bool, error) {
	var count int64
	err := m.DB.Raw(
		"SELECT count(*) FROM information_schema.ins_indexes WHERE db_name = ? AND table_name = ? AND (index_name = ? OR column_name = ?)",
		m.CurrentDatabase(), table, name, column,
	).Scan(&count).Error
	return count > 0, err
}

func (m Migrator) tagIndex(stmt *gorm.Statement, name string) (*schema.Index, error) {
	if stmt.Schema == nil {
		return nil, errors.New("tdengine: failed to parse migration schema")
	}
	index := stmt.Schema.LookIndex(name)
	if index == nil {
		return nil, fmt.Errorf("tdengine: failed to find index %q", name)
	}
	if len(index.Fields) != 1 {
		return nil, ErrSingleTagIndexOnly
	}
	field := index.Fields[0]
	if field.Expression != "" || field.Sort != "" || field.Collate != "" || field.Length != 0 ||
		index.Class != "" || index.Type != "" || index.Where != "" {
		return nil, ErrTagIndexOnly
	}
	if !isTagField(field.Field) {
		return nil, ErrTagIndexOnly
	}
	return index, nil
}

func validateModelIndexes(model *schema.Schema) error {
	for _, index := range model.ParseIndexes() {
		if len(index.Fields) != 1 {
			return ErrSingleTagIndexOnly
		}
		field := index.Fields[0]
		if !isTagField(field.Field) || field.Expression != "" || field.Sort != "" || field.Collate != "" || field.Length != 0 ||
			index.Class != "" || index.Type != "" || index.Where != "" {
			return ErrTagIndexOnly
		}
	}
	return nil
}
