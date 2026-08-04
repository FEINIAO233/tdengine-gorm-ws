package using

import (
	"gorm.io/gorm/clause"
)

type Using struct {
	sTable   string
	tagParis map[string]interface{}
}

func (i Using) Build(builder clause.Builder) {
	builder.WriteString("USING ")
	builder.WriteString(i.sTable)
	var tagValueList = make([]interface{}, 0, len(i.tagParis))
	builder.WriteByte('(')
	index := 0
	for tagName, tagValue := range i.tagParis {
		builder.WriteString(tagName)
		if index < len(i.tagParis)-1 {
			builder.WriteByte(',')
		}
		tagValueList = append(tagValueList, tagValue)
		index++
	}
	builder.WriteByte(')')
	builder.WriteString(" TAGS")
	builder.AddVar(builder, tagValueList)
}

//SetUsing Using clause
func SetUsing(sTable string, tags map[string]interface{}) Using {
	return Using{
		sTable:   sTable,
		tagParis: tags,
	}
}

//ADDTagPair add tag pair to using clause
func (i Using) ADDTagPair(tagName string, tagValue interface{}) Using {
	i.tagParis[tagName] = tagValue
	return i
}

func (i Using) Name() string {
	return "USING"
}

func (i Using) MergeClause(c *clause.Clause) {
	c.Name = ""
	c.Expression = i
}
