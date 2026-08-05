package partition

import "gorm.io/gorm/clause"

// By represents TDengine's PARTITION BY query clause.
type By struct {
	columns     []string
	expressions []clause.Expression
}

// Columns partitions a query by safely quoted column names.
func Columns(names ...string) By {
	return By{columns: names}
}

// Expressions partitions by caller-provided GORM expressions.
func Expressions(expressions ...clause.Expression) By {
	return By{expressions: expressions}
}

func (by By) Name() string { return "PARTITION BY" }

func (by By) Build(builder clause.Builder) {
	written := 0
	for _, column := range by.columns {
		if written > 0 {
			builder.WriteByte(',')
		}
		builder.WriteQuoted(clause.Column{Name: column})
		written++
	}
	for index, expression := range by.expressions {
		if written+index > 0 {
			builder.WriteByte(',')
		}
		expression.Build(builder)
	}
}

func (by By) MergeClause(target *clause.Clause) {
	target.Expression = by
}
