package using_test

import (
	"fmt"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/tests"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/using"
	"gorm.io/gorm/clause"
	"testing"
)

func TestSetValue(t *testing.T) {
	var (
		results = []struct {
			Clauses []clause.Interface
			Result  []string
			Vars    [][][]interface{}
		}{
			{
				Clauses: []clause.Interface{
					clause.Insert{Table: clause.Table{Name: "tb"}},
					using.SetUsing("stb", map[string]interface{}{
						"tag1": 1,
					}).ADDTagPair("tag2", "string"),
				},
				Result: []string{
					"INSERT INTO tb USING stb(?,?) TAGS(?,?)",
				},
				Vars: [][][]interface{}{{{"tag1", "tag2", 1, "string"}, {"tag2", "tag1", "string", 1}}},
			},
		}
	)
	for idx, result := range results {
		t.Run(fmt.Sprintf("TestSetValue case #%v", idx), func(t *testing.T) {
			tests.CheckBuildClauses(t, result.Clauses, result.Result, result.Vars)
		})
	}
}
