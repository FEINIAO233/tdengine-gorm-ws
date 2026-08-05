package create

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

var (
	ErrInvalidTableOption     = errors.New("tdengine: invalid table option")
	ErrInvalidCompression     = errors.New("tdengine: invalid column compression option")
	ErrCommentTooLong         = errors.New("tdengine: table comment exceeds 1024 bytes")
	ErrTTLOnlyForTable        = errors.New("tdengine: TTL is supported only on regular tables and subtables")
	ErrKeepOnlyForStable      = errors.New("tdengine: KEEP is supported only on supertables")
	ErrSMAUnsupportedSubtable = errors.New("tdengine: SMA is not supported on subtables")
)

type Encoding string

const (
	EncodeSimple8B   Encoding = "simple8b"
	EncodeBitPacking Encoding = "bit-packing"
	EncodeDeltaI     Encoding = "delta-i"
	EncodeDeltaD     Encoding = "delta-d"
	EncodeDisabled   Encoding = "disabled"
	EncodeBSS        Encoding = "bss"
)

type Compression string

const (
	CompressLZ4      Compression = "lz4"
	CompressZlib     Compression = "zlib"
	CompressZstd     Compression = "zstd"
	CompressTSZ      Compression = "tsz"
	CompressXZ       Compression = "xz"
	CompressDisabled Compression = "disabled"
)

type CompressionLevel string

const (
	CompressionHigh   CompressionLevel = "high"
	CompressionMedium CompressionLevel = "medium"
	CompressionLow    CompressionLevel = "low"
)

type RetentionUnit string

const (
	RetentionMinutes RetentionUnit = "m"
	RetentionHours   RetentionUnit = "h"
	RetentionDays    RetentionUnit = "d"
)

type Retention struct {
	Value uint64
	Unit  RetentionUnit
}

type TableOptions struct {
	Comment string
	TTL     uint32
	SMA     []string
	Keep    *Retention
}

func (t *Table) WithComment(comment string) *Table {
	t.Options.Comment = comment
	return t
}

func (t *Table) WithTTL(days uint32) *Table {
	t.Options.TTL = days
	return t
}

func (t *Table) WithSMA(columns ...string) *Table {
	t.Options.SMA = append([]string(nil), columns...)
	return t
}

func (t *Table) WithKeep(value uint64, unit RetentionUnit) *Table {
	t.Options.Keep = &Retention{Value: value, Unit: unit}
	return t
}

func validateTableOptions(table *Table) error {
	options := table.Options
	for _, tag := range table.TagColumn {
		if tag != nil && (tag.Encode != "" || tag.Compress != "" || tag.Level != "") {
			return fmt.Errorf("%w: tag %s cannot configure compression", ErrInvalidCompression, tag.Name)
		}
	}
	if len([]byte(options.Comment)) > 1024 {
		return ErrCommentTooLong
	}
	if options.TTL > math.MaxInt32 {
		return fmt.Errorf("%w: TTL must not exceed %d days", ErrInvalidTableOption, math.MaxInt32)
	}
	if table.TableType == STableType && options.TTL != 0 {
		return ErrTTLOnlyForTable
	}
	if table.TableType != STableType && options.Keep != nil {
		return ErrKeepOnlyForStable
	}
	if table.STable != "" && len(options.SMA) > 0 {
		return ErrSMAUnsupportedSubtable
	}
	for _, column := range options.SMA {
		if strings.TrimSpace(column) == "" {
			return fmt.Errorf("%w: SMA column name is empty", ErrInvalidTableOption)
		}
	}
	if options.Keep != nil {
		if options.Keep.Value == 0 {
			return fmt.Errorf("%w: KEEP must be greater than zero", ErrInvalidTableOption)
		}
		switch options.Keep.Unit {
		case RetentionMinutes, RetentionHours, RetentionDays:
		default:
			return fmt.Errorf("%w: invalid KEEP unit %q", ErrInvalidTableOption, options.Keep.Unit)
		}
	}
	return nil
}

func validateCompression(column *Column) error {
	if column.Encode != "" {
		switch column.Encode {
		case EncodeSimple8B, EncodeBitPacking, EncodeDeltaI, EncodeDeltaD, EncodeDisabled, EncodeBSS:
		default:
			return fmt.Errorf("%w: ENCODE %q", ErrInvalidCompression, column.Encode)
		}
	}
	if column.Compress != "" {
		switch column.Compress {
		case CompressLZ4, CompressZlib, CompressZstd, CompressTSZ, CompressXZ, CompressDisabled:
		default:
			return fmt.Errorf("%w: COMPRESS %q", ErrInvalidCompression, column.Compress)
		}
	}
	if column.Level != "" {
		if column.Compress == "" {
			return fmt.Errorf("%w: LEVEL requires COMPRESS", ErrInvalidCompression)
		}
		switch column.Level {
		case CompressionHigh, CompressionMedium, CompressionLow:
		default:
			return fmt.Errorf("%w: LEVEL %q", ErrInvalidCompression, column.Level)
		}
	}
	if column.ColumnType != "" {
		dataType := compressionBaseType(column.ColumnType)
		if column.Encode != "" && !supportsEncoding(dataType, column.Encode) {
			return fmt.Errorf("%w: ENCODE %q is not supported for %s", ErrInvalidCompression, column.Encode, dataType)
		}
		if column.Compress != "" && !supportsCompression(dataType, column.Compress) {
			return fmt.Errorf("%w: COMPRESS %q is not supported for %s", ErrInvalidCompression, column.Compress, dataType)
		}
	}
	return nil
}

func compressionBaseType(dataType string) string {
	dataType = strings.ToUpper(strings.TrimSpace(dataType))
	if index := strings.IndexByte(dataType, '('); index >= 0 {
		dataType = dataType[:index]
	}
	return strings.TrimSpace(dataType)
}

func supportsEncoding(dataType string, encoding Encoding) bool {
	if encoding == EncodeDisabled {
		return true
	}
	switch dataType {
	case IntType, UIntType, TinyIntType, UTinyIntType, SmallIntType, USmallIntType:
		return encoding == EncodeSimple8B
	case BigIntType, UBigIntType, TimestampType:
		return encoding == EncodeSimple8B || encoding == EncodeDeltaI
	case FloatType, DoubleType:
		return encoding == EncodeDeltaD || encoding == EncodeBSS
	case BoolType:
		return encoding == EncodeBitPacking
	case BinaryType, VarcharType, NCharType, DecimalType:
		return false
	default:
		return false
	}
}

func supportsCompression(dataType string, compression Compression) bool {
	if compression == CompressDisabled {
		return true
	}
	switch compression {
	case CompressLZ4, CompressZlib, CompressZstd, CompressXZ:
		switch dataType {
		case TimestampType, BoolType,
			TinyIntType, UTinyIntType, SmallIntType, USmallIntType, IntType, UIntType, BigIntType, UBigIntType,
			FloatType, DoubleType, BinaryType, VarcharType, NCharType, DecimalType:
			return true
		}
	case CompressTSZ:
		return dataType == FloatType || dataType == DoubleType
	}
	return false
}
