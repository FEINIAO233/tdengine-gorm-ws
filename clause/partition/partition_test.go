package partition_test

import (
	"testing"

	"github.com/FEINIAO233/tdengine-gorm-ws/clause/partition"
	"github.com/FEINIAO233/tdengine-gorm-ws/clause/tests"
	"gorm.io/gorm/clause"
)

func TestPartitionBy(t *testing.T) {
	tests.CheckBuildClauses(t, []clause.Interface{
		partition.Columns("tbname", "location"),
	}, []string{"PARTITION BY tbname,location"}, nil)
}
