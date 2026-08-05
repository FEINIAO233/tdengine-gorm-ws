package tdengine_gorm

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"

	"github.com/taosdata/driver-go/v3/common"
	"github.com/taosdata/driver-go/v3/taosWS"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

// DriverName is the default driver name for TDengine.
const DriverName = "taosWS"

// BindMode controls how values are passed to driver-go.
type BindMode uint8

const (
	// BindModeAuto follows GORM PrepareStmt and the interpolateParams DSN option.
	BindModeAuto BindMode = iota
	// BindModeInterpolate encodes string values as TDengine SQL literals.
	BindModeInterpolate
	// BindModePrepared preserves Go values for driver-go prepared statements.
	BindModePrepared
)

type Dialect struct {
	DriverName string
	DSN        string
	Conn       gorm.ConnPool
	BindMode   BindMode
}

func Open(dsn string) gorm.Dialector {
	return &Dialect{DSN: dsn}
}

func (dialect Dialect) Name() string {
	return "tdengine"
}

func (dialect Dialect) Initialize(db *gorm.DB) (err error) {
	if dialect.DriverName == "" {
		dialect.DriverName = DriverName
	}
	db.SkipDefaultTransaction = true
	db.DisableNestedTransaction = true
	db.DisableAutomaticPing = true
	db.DisableForeignKeyConstraintWhenMigrating = true
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{
		LastInsertIDReversed: true,
		QueryClauses:         []string{"SELECT", "FROM", "WHERE", "WINDOW", "FILL", "GROUP BY", "ORDER BY", "SLIMIT", "LIMIT"},
		CreateClauses:        []string{"CREATE TABLE", "INSERT", "USING", "VALUES", "ON CONFLICT"},
	})
	db.Callback().Create().Replace("gorm:create", dialect.Create)
	db.Callback().Update().Replace("gorm:update", dialect.Update)
	db.Callback().Delete().Replace("gorm:delete", dialect.Delete)
	if dialect.Conn != nil {
		db.ConnPool = dialect.Conn
	} else {
		db.ConnPool, err = sql.Open(dialect.DriverName, dialect.DSN)
		if err != nil {
			return err
		}
	}
	for k, v := range dialect.ClauseBuilders() {
		db.ClauseBuilders[k] = v
	}
	return
}

func (dialect Dialect) ClauseBuilders() map[string]clause.ClauseBuilder {
	return map[string]clause.ClauseBuilder{
		"INSERT": func(c clause.Clause, builder clause.Builder) {
			if _, ok := c.Expression.(clause.Insert); ok {
				if stmt, ok := builder.(*gorm.Statement); ok {
					_, containsCreateTable := stmt.Clauses["CREATE TABLE"]
					if containsCreateTable {
						return
					}
				}
			}
			c.Build(builder)
		},
		"FOR": func(c clause.Clause, builder clause.Builder) {
			if _, ok := c.Expression.(clause.Locking); ok {
				return
			}
			c.Build(builder)
		},
		"VALUES": func(c clause.Clause, builder clause.Builder) {
			if values, ok := c.Expression.(clause.Values); ok {
				if stmt, ok := builder.(*gorm.Statement); ok {
					_, containsCreateTable := stmt.Clauses["CREATE TABLE"]
					if containsCreateTable {
						return
					}
				}
				buildValues(values, builder)
				return
			}
			c.Build(builder)
		},
	}
}

func buildValues(values clause.Values, builder clause.Builder) {
	if len(values.Columns) == 0 {
		builder.AddError(errors.New("tdengine: DEFAULT VALUES is not supported"))
		return
	}
	builder.WriteByte('(')
	for index, column := range values.Columns {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteQuoted(column)
	}
	builder.WriteString(") VALUES ")
	for index, row := range values.Values {
		if index > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteByte('(')
		builder.AddVar(builder, row...)
		builder.WriteByte(')')
	}
}

func (dialect Dialect) DefaultValueOf(field *schema.Field) clause.Expression {
	return clause.Expr{SQL: "NULL"}
}

func (dialect Dialect) Migrator(db *gorm.DB) gorm.Migrator {
	return Migrator{migrator.Migrator{Config: migrator.Config{
		DB:                          db,
		Dialector:                   dialect,
		CreateIndexAfterCreateTable: false,
	}}, dialect}
}

func (dialect Dialect) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v interface{}) {
	if !dialect.shouldInterpolate(stmt) {
		writer.WriteByte('?')
		return
	}

	value := v
	if valuer, ok := value.(driver.Valuer); ok {
		converted, err := valuer.Value()
		if err != nil {
			stmt.AddError(err)
		} else {
			value = converted
		}
	}

	// driver-go's interpolator writes strings and byte slices into the SQL
	// verbatim. Supplying a quoted, escaped byte slice here keeps GORM's usual
	// parameter API safe and produces valid TDengine string literals.
	switch value := value.(type) {
	case string:
		stmt.Vars[len(stmt.Vars)-1] = quoteString(value)
	case []byte:
		stmt.Vars[len(stmt.Vars)-1] = quoteString(string(value))
	}
	writer.WriteByte('?')
}

func (dialect Dialect) QuoteTo(writer clause.Writer, str string) {
	var (
		underQuoted, selfQuoted bool
		continuousBacktick      int8
		shiftDelimiter          int8
	)

	for _, character := range []byte(str) {
		switch character {
		case '`':
			continuousBacktick++
			if continuousBacktick == 2 {
				writer.WriteString("``")
				continuousBacktick = 0
			}
		case '.':
			if continuousBacktick > 0 || !selfQuoted {
				shiftDelimiter = 0
				underQuoted = false
				continuousBacktick = 0
				writer.WriteByte('`')
			}
			writer.WriteByte(character)
			continue
		default:
			if shiftDelimiter-continuousBacktick <= 0 && !underQuoted {
				writer.WriteByte('`')
				underQuoted = true
				if selfQuoted = continuousBacktick > 0; selfQuoted {
					continuousBacktick--
				}
			}

			for ; continuousBacktick > 0; continuousBacktick-- {
				writer.WriteString("``")
			}
			writer.WriteByte(character)
		}
		shiftDelimiter++
	}

	if continuousBacktick > 0 && !selfQuoted {
		writer.WriteString("``")
	}
	writer.WriteByte('`')
}

func (dialect Dialect) shouldInterpolate(stmt *gorm.Statement) bool {
	switch dialect.BindMode {
	case BindModeInterpolate:
		return true
	case BindModePrepared:
		return false
	}
	if stmt != nil && stmt.DB != nil && stmt.DB.PrepareStmt {
		return false
	}
	if dialect.DSN != "" {
		if config, err := taosWS.ParseDSN(dialect.DSN); err == nil {
			return config.InterpolateParams
		}
	}
	return true
}

type sqlLiteral string

func (literal sqlLiteral) Value() (driver.Value, error) {
	return []byte(literal), nil
}

func quoteString(value string) sqlLiteral {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return sqlLiteral("'" + replacer.Replace(value) + "'")
}

func (dialect Dialect) Explain(sql string, vars ...interface{}) string {
	args := make([]driver.NamedValue, len(vars))
	hasLiteral := false
	for index, value := range vars {
		if literal, ok := value.(sqlLiteral); ok {
			value = []byte(literal)
			hasLiteral = true
		}
		args[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	if hasLiteral {
		if explained, err := common.InterpolateParams(sql, args); err == nil {
			return explained
		}
	}
	return logger.ExplainSQL(sql, nil, "'", vars...)
}

func (dialect Dialect) DataTypeOf(field *schema.Field) string {
	switch field.DataType {
	case schema.Bool:
		return "bool"
	case schema.Int:
		sqlType := "bigint"
		switch {
		case field.Size <= 8:
			sqlType = "tinyint"
		case field.Size <= 16:
			sqlType = "smallint"
		case field.Size <= 32:
			sqlType = "int"
		}
		return sqlType
	case schema.Uint:
		sqlType := "bigint unsigned"
		switch {
		case field.Size <= 8:
			sqlType = "tinyint unsigned"
		case field.Size <= 16:
			sqlType = "smallint unsigned"
		case field.Size <= 32:
			sqlType = "int unsigned"
		}
		return sqlType
	case schema.Float:
		if field.Size <= 32 {
			return "float"
		}
		return "double"
	case schema.String:
		size := field.Size
		if size == 0 {
			size = 64
		}
		return fmt.Sprintf("NCHAR(%d)", size)
	case schema.Time:
		return "TIMESTAMP"
	case schema.Bytes:
		size := field.Size
		if size == 0 {
			size = 64
		}
		return fmt.Sprintf("BINARY(%d)", size)
	}

	return string(field.DataType)
}

func (dialect Dialect) SavePoint(tx *gorm.DB, name string) error {
	return errors.New("not support")
}

func (dialect Dialect) RollbackTo(tx *gorm.DB, name string) error {
	return errors.New("not support")
}
