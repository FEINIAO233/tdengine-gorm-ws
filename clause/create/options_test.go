package create

import (
	"errors"
	"strings"
	"testing"
)

func TestTableOptionValidation(t *testing.T) {
	stable := NewSTable("metrics", true, nil, nil).WithTTL(30)
	if err := validateTableOptions(stable); !errors.Is(err, ErrTTLOnlyForTable) {
		t.Fatalf("expected ErrTTLOnlyForTable, got %v", err)
	}
	table := NewTable("metrics", true, nil, "", nil).WithKeep(30, RetentionDays)
	if err := validateTableOptions(table); !errors.Is(err, ErrKeepOnlyForStable) {
		t.Fatalf("expected ErrKeepOnlyForStable, got %v", err)
	}
	subtable := NewTable("device-1", true, nil, "metrics", nil).WithSMA("value")
	if err := validateTableOptions(subtable); !errors.Is(err, ErrSMAUnsupportedSubtable) {
		t.Fatalf("expected ErrSMAUnsupportedSubtable, got %v", err)
	}
	longComment := NewTable("metrics", true, nil, "", nil).WithComment(strings.Repeat("a", 1025))
	if err := validateTableOptions(longComment); !errors.Is(err, ErrCommentTooLong) {
		t.Fatalf("expected ErrCommentTooLong, got %v", err)
	}
	compressedTag := NewSTable("metrics", true, nil, []*Column{{Name: "location", ColumnType: VarcharType, Compress: CompressZstd}})
	if err := validateTableOptions(compressedTag); !errors.Is(err, ErrInvalidCompression) {
		t.Fatalf("expected ErrInvalidCompression, got %v", err)
	}
}

func TestCompressionValidation(t *testing.T) {
	valid := &Column{ColumnType: DoubleType, Compress: CompressZstd, Level: CompressionHigh, Encode: EncodeBSS}
	if err := validateCompression(valid); err != nil {
		t.Fatalf("validate compression: %v", err)
	}
	for _, column := range []*Column{
		{Encode: Encoding("invalid")},
		{Compress: Compression("invalid")},
		{Level: CompressionHigh},
		{Compress: CompressZstd, Level: CompressionLevel("invalid")},
		{ColumnType: IntType, Encode: EncodeBSS},
		{ColumnType: VarcharType, Compress: CompressTSZ},
	} {
		if err := validateCompression(column); !errors.Is(err, ErrInvalidCompression) {
			t.Fatalf("expected ErrInvalidCompression for %#v, got %v", column, err)
		}
	}
}
