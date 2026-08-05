package create

import (
	"sort"
	"strconv"

	"gorm.io/gorm/clause"
)

type CreateTable struct {
	tables []*Table
}

const (
	STableType = iota + 1
	CommonTableType
)

type Table struct {
	TableType   int
	Table       string
	IfNotExists bool
	STable      string
	Tags        map[string]interface{}
	Column      []*Column
	TagColumn   []*Column
}

// NewTable Create new common table
func NewTable(name string, ifNotExist bool, column []*Column, Stable string, tags map[string]interface{}) *Table {
	return &Table{
		TableType:   CommonTableType,
		Table:       name,
		IfNotExists: ifNotExist,
		STable:      Stable,
		Tags:        tags,
		Column:      column,
	}
}

// NewSTable Create new sTable
func NewSTable(name string, ifNotExists bool, column []*Column, tagColumn []*Column) *Table {
	return &Table{
		TableType:   STableType,
		Table:       name,
		IfNotExists: ifNotExists,
		Column:      column,
		TagColumn:   tagColumn,
	}
}

// NewCreateTableClause Create table clause
func NewCreateTableClause(tables []*Table) CreateTable {
	return CreateTable{tables: tables}
}

// AddTables Add tables to clause
func (c CreateTable) AddTables(tables ...*Table) CreateTable {
	c.tables = append(c.tables, tables...)
	return c
}

type Column struct {
	Name       string
	ColumnType string
	Length     uint64
}

const (
	TimestampType = "TIMESTAMP"
	IntType       = "INT"
	BigIntType    = "BIGINT"
	FloatType     = "FLOAT"
	DoubleType    = "DOUBLE"
	BinaryType    = "BINARY"
	SmallIntType  = "SMALLINT"
	TinyIntType   = "TINYINT"
	UTinyIntType  = "TINYINT UNSIGNED"
	USmallIntType = "SMALLINT UNSIGNED"
	UIntType      = "INT UNSIGNED"
	UBigIntType   = "BIGINT UNSIGNED"
	BoolType      = "BOOL"
	NCharType     = "NCHAR"
	VarcharType   = "VARCHAR"
	VarBinaryType = "VARBINARY"
)

// Build renders a TDengine column definition. Implementing clause.Expression
// also lets the migrator reuse the same definition in ALTER statements.
func (c *Column) Build(builder clause.Builder) {
	builder.WriteQuoted(clause.Column{Name: c.Name})
	builder.WriteByte(' ')
	builder.WriteString(c.ColumnType)
	if c.ColumnType == NCharType || c.ColumnType == BinaryType || c.ColumnType == VarcharType || c.ColumnType == VarBinaryType {
		builder.WriteByte('(')
		builder.WriteString(strconv.FormatUint(c.Length, 10))
		builder.WriteByte(')')
	}
}

func (CreateTable) Name() string {
	return "CREATE TABLE"
}

func (c CreateTable) Build(builder clause.Builder) {
	if len(c.tables) > 1 && allSubtables(c.tables) {
		builder.WriteString("CREATE TABLE ")
		for tableIndex, table := range c.tables {
			if tableIndex > 0 {
				builder.WriteByte(' ')
			}
			buildTable(builder, table, false)
		}
		return
	}

	for tableIndex, table := range c.tables {
		if tableIndex > 0 {
			builder.WriteByte(' ')
		}
		buildTable(builder, table, true)
	}
}

func allSubtables(tables []*Table) bool {
	for _, table := range tables {
		if table == nil || table.TableType != CommonTableType || table.STable == "" {
			return false
		}
	}
	return true
}

func buildTable(builder clause.Builder, table *Table, writeCommand bool) {
	if table == nil {
		return
	}
	if writeCommand {
		switch table.TableType {
		case CommonTableType:
			builder.WriteString("CREATE TABLE ")
		case STableType:
			builder.WriteString("CREATE STABLE ")
		default:
			return
		}
	}
	if table.IfNotExists {
		builder.WriteString("IF NOT EXISTS ")
	}
	builder.WriteQuoted(clause.Table{Name: table.Table})
	if table.TableType == CommonTableType && table.STable != "" {
		builder.WriteString(" USING ")
		builder.WriteQuoted(clause.Table{Name: table.STable})
		tagValueList := make([]interface{}, 0, len(table.Tags))
		tagNames := make([]string, 0, len(table.Tags))
		for tag := range table.Tags {
			tagNames = append(tagNames, tag)
		}
		sort.Strings(tagNames)
		if len(tagNames) > 0 {
			builder.WriteByte('(')
		}
		for index, tag := range tagNames {
			builder.WriteQuoted(clause.Column{Name: tag})
			if index != len(tagNames)-1 {
				builder.WriteByte(',')
			}
			tagValueList = append(tagValueList, table.Tags[tag])
		}
		if len(tagNames) > 0 {
			builder.WriteByte(')')
		}
		builder.WriteString(" TAGS ")
		builder.AddVar(builder, tagValueList)
	} else {
		builder.WriteString(" (")
		for i, column := range table.Column {
			column.Build(builder)
			if i != len(table.Column)-1 {
				builder.WriteByte(',')
			}
		}
		builder.WriteByte(')')
	}
	if table.TableType == STableType {
		builder.WriteString(" TAGS(")
		for i, tags := range table.TagColumn {
			tags.Build(builder)
			if i != len(table.TagColumn)-1 {
				builder.WriteByte(',')
			}
		}
		builder.WriteByte(')')
	}
}

// MergeClause merge CREATE TABLE by clauses
func (c CreateTable) MergeClause(clause *clause.Clause) {
	clause.Name = ""
	clause.Expression = c
}
