package tdengine_gorm

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	createclause "github.com/FEINIAO233/tdengine-gorm-ws/clause/create"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

var (
	ErrTimestampFirst         = errors.New("tdengine: the first data column must be TIMESTAMP")
	ErrNoDataColumns          = errors.New("tdengine: a table requires at least one data column")
	ErrConstraintsUnsupported = errors.New("tdengine: GORM constraints are not supported")
	ErrRenameTableUnsupported = errors.New("tdengine: renaming tables through GORM is not supported")
)

// Migrator implements the subset of GORM migration operations that maps to
// TDengine tables and supertables. AutoMigrate is intentionally additive: it
// creates missing objects and adds missing columns/tags, but never drops or
// changes existing definitions.
type Migrator struct {
	migrator.Migrator
	d Dialect
}

type tdTableType struct {
	schema   string
	name     string
	typeName string
	comment  sql.NullString
}

func (table tdTableType) Schema() string { return table.schema }
func (table tdTableType) Name() string   { return table.name }
func (table tdTableType) Type() string   { return table.typeName }
func (table tdTableType) Comment() (string, bool) {
	return table.comment.String, table.comment.Valid
}

func (m Migrator) FullDataTypeOf(field *schema.Field) clause.Expr {
	return clause.Expr{SQL: m.d.DataTypeOf(field)}
}

func (m Migrator) HasTable(value interface{}) bool {
	found := false
	_ = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		database := m.CurrentDatabase()
		var count int64
		if err := m.DB.Raw(
			"SELECT count(*) FROM information_schema.ins_tables WHERE db_name = ? AND table_name = ?",
			database, stmt.Table,
		).Scan(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if err := m.DB.Raw(
				"SELECT count(*) FROM information_schema.ins_stables WHERE db_name = ? AND stable_name = ?",
				database, stmt.Table,
			).Scan(&count).Error; err != nil {
				return err
			}
		}
		found = count > 0
		return nil
	})
	return found
}

func (m Migrator) GetTables() ([]string, error) {
	database := m.CurrentDatabase()
	var tables, stables []string
	if err := m.DB.Raw(
		"SELECT table_name FROM information_schema.ins_tables WHERE db_name = ?", database,
	).Scan(&tables).Error; err != nil {
		return nil, err
	}
	if err := m.DB.Raw(
		"SELECT stable_name FROM information_schema.ins_stables WHERE db_name = ?", database,
	).Scan(&stables).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(tables)+len(stables))
	result := make([]string, 0, len(tables)+len(stables))
	for _, name := range append(tables, stables...) {
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func (m Migrator) TableType(value interface{}) (result gorm.TableType, err error) {
	err = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		database := m.CurrentDatabase()
		var stable struct {
			Name    string         `gorm:"column:stable_name"`
			Comment sql.NullString `gorm:"column:table_comment"`
		}
		if queryErr := m.DB.Raw(
			"SELECT stable_name, table_comment FROM information_schema.ins_stables WHERE db_name = ? AND stable_name = ?",
			database, stmt.Table,
		).Scan(&stable).Error; queryErr != nil {
			return queryErr
		}
		if stable.Name != "" {
			result = tdTableType{schema: database, name: stable.Name, typeName: "SUPER TABLE", comment: stable.Comment}
			return nil
		}

		var table struct {
			Name    string         `gorm:"column:table_name"`
			Type    string         `gorm:"column:type"`
			Comment sql.NullString `gorm:"column:table_comment"`
		}
		if queryErr := m.DB.Raw(
			"SELECT table_name, type, table_comment FROM information_schema.ins_tables WHERE db_name = ? AND table_name = ?",
			database, stmt.Table,
		).Scan(&table).Error; queryErr != nil {
			return queryErr
		}
		if table.Name == "" {
			return fmt.Errorf("tdengine: table %s does not exist", stmt.Table)
		}
		result = tdTableType{schema: database, name: table.Name, typeName: table.Type, comment: table.Comment}
		return nil
	})
	return result, err
}

func (m Migrator) HasColumn(value interface{}, name string) bool {
	found := false
	_ = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(name); field != nil {
				name = field.DBName
			}
		}
		database := m.CurrentDatabase()
		var count int64
		if err := m.DB.Raw(
			"SELECT count(*) FROM information_schema.ins_columns WHERE db_name = ? AND table_name = ? AND col_name = ?",
			database, stmt.Table, name,
		).Scan(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			var description []struct {
				Field string `gorm:"column:field"`
			}
			if err := m.DB.Raw(
				"DESCRIBE ?", clause.Table{Name: stmt.Table},
			).Scan(&description).Error; err != nil {
				return err
			}
			for _, field := range description {
				if strings.EqualFold(field.Field, name) {
					count = 1
					break
				}
			}
		}
		found = count > 0
		return nil
	})
	return found
}

func (m Migrator) CreateTable(values ...interface{}) error {
	for _, value := range m.ReorderModels(values, false) {
		if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
			if stmt.Schema == nil {
				return errors.New("tdengine: failed to parse migration schema")
			}
			columns, tags := m.modelColumns(stmt.Schema)
			if err := validateModelColumns(columns, tags); err != nil {
				return fmt.Errorf("%s: %w", stmt.Table, err)
			}
			if err := validateModelIndexes(stmt.Schema); err != nil {
				return err
			}

			var table *createclause.Table
			if len(tags) > 0 {
				table = createclause.NewSTable(stmt.Table, true, columns, tags)
			} else {
				table = createclause.NewTable(stmt.Table, true, columns, "", nil)
			}
			if err := m.DB.Table(stmt.Table).
				Clauses(createclause.NewCreateTableClause([]*createclause.Table{table})).
				Create(map[string]interface{}{}).Error; err != nil {
				return err
			}
			return m.createMissingIndexes(value)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m Migrator) AutoMigrate(values ...interface{}) error {
	for _, value := range m.ReorderModels(values, true) {
		if !m.HasTable(value) {
			if err := m.CreateTable(value); err != nil {
				return err
			}
			continue
		}
		if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
			if stmt.Schema == nil {
				return errors.New("tdengine: failed to parse migration schema")
			}
			columns, tags := m.modelColumns(stmt.Schema)
			if err := validateModelColumns(columns, tags); err != nil {
				return fmt.Errorf("%s: %w", stmt.Table, err)
			}
			if err := validateModelIndexes(stmt.Schema); err != nil {
				return err
			}
			stable, err := m.isStable(stmt.Table)
			if err != nil {
				return err
			}
			for _, field := range stmt.Schema.Fields {
				if field.IgnoreMigration || field.DBName == "" || m.HasColumn(value, field.DBName) {
					continue
				}
				column := m.columnDefinition(field)
				if isTagField(field) {
					if !stable {
						return fmt.Errorf("tdengine: field %s is a tag but %s is not a supertable", field.Name, stmt.Table)
					}
					if err := m.AddStableTag(stmt.Table, column); err != nil {
						return err
					}
				} else if stable {
					if err := m.AddStableColumn(stmt.Table, column); err != nil {
						return err
					}
				} else if err := m.addTableColumn(stmt.Table, column); err != nil {
					return err
				}
			}
			return m.createMissingIndexes(value)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m Migrator) AddColumn(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema == nil {
			return errors.New("tdengine: failed to parse migration schema")
		}
		field := stmt.Schema.LookUpField(name)
		if field == nil {
			return fmt.Errorf("tdengine: failed to look up field %q", name)
		}
		if field.IgnoreMigration {
			return nil
		}
		stable, err := m.isStable(stmt.Table)
		if err != nil {
			return err
		}
		column := m.columnDefinition(field)
		if err := validateColumnPlacement(column, isTagField(field)); err != nil {
			return err
		}
		if isTagField(field) {
			if !stable {
				return fmt.Errorf("tdengine: cannot add a tag to regular table %s", stmt.Table)
			}
			return m.AddStableTag(stmt.Table, column)
		}
		if stable {
			return m.AddStableColumn(stmt.Table, column)
		}
		return m.addTableColumn(stmt.Table, column)
	})
}

func (m Migrator) DropColumn(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		var field *schema.Field
		if stmt.Schema != nil {
			field = stmt.Schema.LookUpField(name)
			if field != nil {
				name = field.DBName
			}
		}
		stable, err := m.isStable(stmt.Table)
		if err != nil {
			return err
		}
		if stable && field != nil && isTagField(field) {
			return m.DropStableTag(stmt.Table, name)
		}
		command := "ALTER TABLE ? DROP COLUMN ?"
		if stable {
			command = "ALTER STABLE ? DROP COLUMN ?"
		}
		return m.DB.Exec(command, clause.Table{Name: stmt.Table}, clause.Column{Name: name}).Error
	})
}

func (m Migrator) AlterColumn(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema == nil {
			return errors.New("tdengine: failed to parse migration schema")
		}
		field := stmt.Schema.LookUpField(name)
		if field == nil {
			return fmt.Errorf("tdengine: failed to look up field %q", name)
		}
		stable, err := m.isStable(stmt.Table)
		if err != nil {
			return err
		}
		column := m.columnDefinition(field)
		if stable && isTagField(field) {
			return m.ModifyStableTag(stmt.Table, column)
		}
		command := "ALTER TABLE ? MODIFY COLUMN ?"
		if stable {
			command = "ALTER STABLE ? MODIFY COLUMN ?"
		}
		return m.DB.Exec(command, clause.Table{Name: stmt.Table}, column).Error
	})
}

func (m Migrator) DropTable(values ...interface{}) error {
	values = m.ReorderModels(values, false)
	for index := len(values) - 1; index >= 0; index-- {
		if err := m.RunWithValue(values[index], func(stmt *gorm.Statement) error {
			stable, err := m.isStable(stmt.Table)
			if err != nil {
				return err
			}
			command := "DROP TABLE IF EXISTS ?"
			if stable {
				command = "DROP STABLE IF EXISTS ?"
			}
			return m.DB.Exec(command, clause.Table{Name: stmt.Table}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

// MigrateColumn is a no-op because automatic type changes can be destructive
// in TDengine. Call AlterColumn explicitly after reviewing the change.
func (m Migrator) MigrateColumn(interface{}, *schema.Field, gorm.ColumnType) error { return nil }

func (m Migrator) MigrateColumnUnique(interface{}, *schema.Field, gorm.ColumnType) error {
	return nil
}

func (m Migrator) RenameColumn(interface{}, string, string) error {
	return errors.New("tdengine: renaming data columns is not supported")
}

func (m Migrator) RenameIndex(interface{}, string, string) error {
	return errors.New("tdengine: tag indexes cannot be renamed; drop and recreate the index")
}

func (m Migrator) DropConstraint(interface{}, string) error {
	return ErrConstraintsUnsupported
}

func (m Migrator) CreateConstraint(interface{}, string) error { return ErrConstraintsUnsupported }

func (m Migrator) HasConstraint(interface{}, string) bool { return false }

func (m Migrator) RenameTable(interface{}, interface{}) error { return ErrRenameTableUnsupported }

func (m Migrator) AddStableColumn(stable string, column *createclause.Column) error {
	return m.DB.Exec("ALTER STABLE ? ADD COLUMN ?", clause.Table{Name: stable}, column).Error
}

func (m Migrator) DropStableColumn(stable, column string) error {
	return m.DB.Exec("ALTER STABLE ? DROP COLUMN ?", clause.Table{Name: stable}, clause.Column{Name: column}).Error
}

func (m Migrator) ModifyStableColumn(stable string, column *createclause.Column) error {
	return m.DB.Exec("ALTER STABLE ? MODIFY COLUMN ?", clause.Table{Name: stable}, column).Error
}

func (m Migrator) AddStableTag(stable string, tag *createclause.Column) error {
	return m.DB.Exec("ALTER STABLE ? ADD TAG ?", clause.Table{Name: stable}, tag).Error
}

func (m Migrator) DropStableTag(stable, tag string) error {
	return m.DB.Exec("ALTER STABLE ? DROP TAG ?", clause.Table{Name: stable}, clause.Column{Name: tag}).Error
}

func (m Migrator) ModifyStableTag(stable string, tag *createclause.Column) error {
	return m.DB.Exec("ALTER STABLE ? MODIFY TAG ?", clause.Table{Name: stable}, tag).Error
}

func (m Migrator) RenameStableTag(stable, oldName, newName string) error {
	return m.DB.Exec(
		"ALTER STABLE ? RENAME TAG ? ?",
		clause.Table{Name: stable}, clause.Column{Name: oldName}, clause.Column{Name: newName},
	).Error
}

func (m Migrator) SetTableTag(table, tag string, value interface{}) error {
	return m.DB.Exec(
		"ALTER TABLE ? SET TAG ? = ?",
		clause.Table{Name: table}, clause.Column{Name: tag}, value,
	).Error
}

func (m Migrator) addTableColumn(table string, column *createclause.Column) error {
	return m.DB.Exec("ALTER TABLE ? ADD COLUMN ?", clause.Table{Name: table}, column).Error
}

func (m Migrator) isStable(table string) (bool, error) {
	var count int64
	err := m.DB.Raw(
		"SELECT count(*) FROM information_schema.ins_stables WHERE db_name = ? AND stable_name = ?",
		m.CurrentDatabase(), table,
	).Scan(&count).Error
	return count > 0, err
}

func (m Migrator) modelColumns(model *schema.Schema) (columns, tags []*createclause.Column) {
	for _, field := range model.Fields {
		if field.IgnoreMigration || field.DBName == "" {
			continue
		}
		column := m.columnDefinition(field)
		if isTagField(field) {
			tags = append(tags, column)
		} else {
			columns = append(columns, column)
		}
	}
	return columns, tags
}

func (m Migrator) columnDefinition(field *schema.Field) *createclause.Column {
	return &createclause.Column{Name: field.DBName, ColumnType: m.d.DataTypeOf(field)}
}

func isTagField(field *schema.Field) bool {
	for _, option := range strings.Split(field.StructField.Tag.Get("tdengine"), ",") {
		if strings.EqualFold(strings.TrimSpace(option), "tag") {
			return true
		}
	}
	return false
}

func validateDataColumns(columns []*createclause.Column) error {
	if len(columns) == 0 {
		return ErrNoDataColumns
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(columns[0].ColumnType)), createclause.TimestampType) {
		return ErrTimestampFirst
	}
	return nil
}

func validateModelColumns(columns, tags []*createclause.Column) error {
	if err := validateDataColumns(columns); err != nil {
		return err
	}
	blobCount := 0
	for _, column := range columns {
		if err := validateColumnPlacement(column, false); err != nil {
			return err
		}
		typeName := baseDataType(column.ColumnType)
		if typeName == createclause.BlobType {
			blobCount++
		}
	}
	if blobCount > 1 {
		return errors.New("tdengine: only one BLOB column is allowed per table")
	}
	for _, tag := range tags {
		if err := validateColumnPlacement(tag, true); err != nil {
			return err
		}
	}
	if len(tags) > 128 {
		return errors.New("tdengine: a supertable supports at most 128 tags")
	}
	return nil
}

func validateColumnPlacement(column *createclause.Column, tag bool) error {
	typeName := baseDataType(column.ColumnType)
	if !tag && typeName == createclause.JSONType {
		return fmt.Errorf("tdengine: JSON field %s must be a tag", column.Name)
	}
	if tag && (typeName == createclause.DecimalType || typeName == createclause.BlobType) {
		return fmt.Errorf("tdengine: %s is not supported for tag %s", typeName, column.Name)
	}
	return nil
}

func baseDataType(dataType string) string {
	dataType = strings.ToUpper(strings.TrimSpace(dataType))
	if index := strings.IndexByte(dataType, '('); index >= 0 {
		dataType = dataType[:index]
	}
	return strings.TrimSpace(dataType)
}

// ColumnTypes reads TDengine's native metadata instead of the MySQL-style
// information_schema tables used by GORM's default migrator.
func (m Migrator) ColumnTypes(value interface{}) (result []gorm.ColumnType, err error) {
	err = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		var rows []struct {
			Name      string         `gorm:"column:col_name"`
			Type      string         `gorm:"column:col_type"`
			Length    sql.NullInt64  `gorm:"column:col_length"`
			Precision sql.NullInt64  `gorm:"column:col_precision"`
			Scale     sql.NullInt64  `gorm:"column:col_scale"`
			Nullable  sql.NullString `gorm:"column:col_nullable"`
		}
		if queryErr := m.DB.Raw(
			"SELECT col_name, col_type, col_length, col_precision, col_scale, col_nullable FROM information_schema.ins_columns WHERE db_name = ? AND table_name = ? ORDER BY col_name",
			m.CurrentDatabase(), stmt.Table,
		).Scan(&rows).Error; queryErr != nil {
			return queryErr
		}
		for _, row := range rows {
			nullable := sql.NullBool{}
			if row.Nullable.Valid {
				nullable = sql.NullBool{Bool: strings.EqualFold(row.Nullable.String, "YES") || row.Nullable.String == "1" || strings.EqualFold(row.Nullable.String, "true"), Valid: true}
			}
			result = append(result, migrator.ColumnType{
				NameValue:        sql.NullString{String: row.Name, Valid: true},
				DataTypeValue:    sql.NullString{String: row.Type, Valid: true},
				ColumnTypeValue:  sql.NullString{String: row.Type, Valid: true},
				LengthValue:      row.Length,
				DecimalSizeValue: row.Precision,
				ScaleValue:       row.Scale,
				NullableValue:    nullable,
			})
		}
		return nil
	})
	return result, err
}
