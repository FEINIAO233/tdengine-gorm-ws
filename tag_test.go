package tdengine_gorm

import (
	"errors"
	"reflect"
	"testing"
)

func TestBuildTableTagUpdateSQL(t *testing.T) {
	statement, values, err := buildTableTagUpdateSQL(Dialect{}, []TableTagUpdate{
		{Table: "power.device-1", Tags: map[string]interface{}{"location": "Shanghai", "group": 7}},
		{Table: "device-2", Tags: map[string]interface{}{"group": 8}},
	})
	if err != nil {
		t.Fatalf("build batch tag update: %v", err)
	}
	const expected = "ALTER TABLE `power`.`device-1` SET TAG `group` = ?,`location` = ? `device-2` SET TAG `group` = ?"
	if statement != expected {
		t.Fatalf("expected %q, got %q", expected, statement)
	}
	if expectedValues := []interface{}{7, "Shanghai", 8}; !reflect.DeepEqual(values, expectedValues) {
		t.Fatalf("expected values %#v, got %#v", expectedValues, values)
	}
}

func TestBuildTableTagUpdateSQLValidation(t *testing.T) {
	for _, test := range []struct {
		updates []TableTagUpdate
		err     error
	}{
		{err: ErrTagUpdateRequired},
		{updates: []TableTagUpdate{{Table: "device-1"}}, err: ErrTagValueRequired},
		{updates: []TableTagUpdate{
			{Table: "device-1", Tags: map[string]interface{}{"group": 1}},
			{Table: "DEVICE-1", Tags: map[string]interface{}{"group": 2}},
		}, err: ErrDuplicateTagUpdateTable},
	} {
		_, _, err := buildTableTagUpdateSQL(Dialect{}, test.updates)
		if !errors.Is(err, test.err) {
			t.Fatalf("expected %v, got %v", test.err, err)
		}
	}
}
