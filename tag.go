package tdengine_gorm

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

var (
	ErrTagUpdateRequired       = errors.New("tdengine: at least one table tag update is required")
	ErrTagValueRequired        = errors.New("tdengine: at least one tag value is required")
	ErrDuplicateTagUpdateTable = errors.New("tdengine: a table may appear only once in a batch tag update")
)

type TableTagUpdate struct {
	Table string
	Tags  map[string]interface{}
}

func (m Migrator) SetTableTags(table string, tags map[string]interface{}) error {
	return m.SetTableTagsBatch(TableTagUpdate{Table: table, Tags: tags})
}

func (m Migrator) SetTableTagsBatch(updates ...TableTagUpdate) error {
	if len(updates) == 0 {
		return ErrTagUpdateRequired
	}
	for _, update := range updates {
		if err := m.ensurePhysicalTable(update.Table); err != nil {
			return err
		}
	}
	statement, values, err := buildTableTagUpdateSQL(m.DB.Dialector, updates)
	if err != nil {
		return err
	}
	return m.DB.Exec(statement, values...).Error
}

func buildTableTagUpdateSQL(dialector gorm.Dialector, updates []TableTagUpdate) (string, []interface{}, error) {
	if len(updates) == 0 {
		return "", nil, ErrTagUpdateRequired
	}
	var statement strings.Builder
	statement.WriteString("ALTER TABLE ")
	values := make([]interface{}, 0)
	seenTables := make(map[string]struct{}, len(updates))
	for updateIndex, update := range updates {
		if strings.TrimSpace(update.Table) == "" {
			return "", nil, fmt.Errorf("%w: table name is empty", ErrTagUpdateRequired)
		}
		if len(update.Tags) == 0 {
			return "", nil, fmt.Errorf("%w: %s", ErrTagValueRequired, update.Table)
		}
		tableKey := strings.ToLower(update.Table)
		if _, exists := seenTables[tableKey]; exists {
			return "", nil, fmt.Errorf("%w: %s", ErrDuplicateTagUpdateTable, update.Table)
		}
		seenTables[tableKey] = struct{}{}
		if updateIndex > 0 {
			statement.WriteByte(' ')
		}
		dialector.QuoteTo(&statement, update.Table)
		statement.WriteString(" SET TAG ")

		tagNames := make([]string, 0, len(update.Tags))
		for name := range update.Tags {
			if strings.TrimSpace(name) == "" {
				return "", nil, fmt.Errorf("%w: tag name is empty", ErrTagValueRequired)
			}
			tagNames = append(tagNames, name)
		}
		sort.Strings(tagNames)
		for tagIndex, name := range tagNames {
			if tagIndex > 0 {
				statement.WriteByte(',')
			}
			dialector.QuoteTo(&statement, name)
			statement.WriteString(" = ?")
			values = append(values, update.Tags[name])
		}
	}
	return statement.String(), values, nil
}
