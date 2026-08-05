package using

import (
	"sort"

	"gorm.io/gorm/clause"
)

// Tag associates a supertable tag column with its value.
type Tag struct {
	Name  string
	Value interface{}
}

type Using struct {
	sTable string
	tags   []Tag
}

func (i Using) Build(builder clause.Builder) {
	builder.WriteString("USING ")
	builder.WriteQuoted(clause.Table{Name: i.sTable})
	var tagValueList = make([]interface{}, 0, len(i.tags))
	if len(i.tags) > 0 {
		builder.WriteByte('(')
	}
	for index, tag := range i.tags {
		builder.WriteQuoted(clause.Column{Name: tag.Name})
		if index < len(i.tags)-1 {
			builder.WriteByte(',')
		}
		tagValueList = append(tagValueList, tag.Value)
	}
	if len(i.tags) > 0 {
		builder.WriteByte(')')
	}
	builder.WriteString(" TAGS")
	builder.AddVar(builder, tagValueList)
}

// SetUsing creates a USING clause. Map keys are sorted to make the generated
// SQL deterministic while preserving the association between names and values.
func SetUsing(sTable string, tags map[string]interface{}) Using {
	names := make([]string, 0, len(tags))
	for name := range tags {
		names = append(names, name)
	}
	sort.Strings(names)

	ordered := make([]Tag, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, Tag{Name: name, Value: tags[name]})
	}
	return SetUsingTags(sTable, ordered...)
}

// SetUsingTags creates a USING clause with an explicit tag order.
func SetUsingTags(sTable string, tags ...Tag) Using {
	return Using{sTable: sTable, tags: append([]Tag(nil), tags...)}
}

// AddTag adds or replaces a tag without mutating the original clause value.
func (i Using) AddTag(tagName string, tagValue interface{}) Using {
	tags := append([]Tag(nil), i.tags...)
	for index := range tags {
		if tags[index].Name == tagName {
			tags[index].Value = tagValue
			i.tags = tags
			return i
		}
	}
	i.tags = append(tags, Tag{Name: tagName, Value: tagValue})
	return i
}

// ADDTagPair is kept for backward compatibility. Deprecated: use AddTag.
func (i Using) ADDTagPair(tagName string, tagValue interface{}) Using {
	return i.AddTag(tagName, tagValue)
}

func (i Using) Name() string {
	return "USING"
}

func (i Using) MergeClause(c *clause.Clause) {
	c.Name = ""
	c.Expression = i
}
