// Package bulk reads and writes record sets as CSV, for importing and
// exporting many records at once.
package bulk

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kerem/unbound-dns/internal/records"
)

// Columns are matched by header name, so the order in the file is free.
const (
	colName  = "name"
	colType  = "type"
	colValue = "value"
	colTTL   = "ttl"
)

// Header is what Export writes and what Import expects to find.
var Header = []string{colName, colType, colValue, colTTL}

// RowError is a single line that could not be read.
type RowError struct {
	// Line is the 1-based line number in the file, counting the header.
	Line int
	Err  error
}

func (e RowError) Error() string {
	return fmt.Sprintf("satır %d: %s", e.Line, e.Err)
}

// ImportResult carries the records that parsed and the rows that did not.
//
// A single bad row does not reject the file: the valid rows are still usable,
// and reporting each failure with its line number is more useful than refusing
// everything over one typo.
type ImportResult struct {
	Records []records.Record
	Errors  []RowError
}

// Import reads records from CSV.
func Import(r io.Reader) (ImportResult, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	// Rows may legitimately omit trailing columns, so the field count is
	// checked per row instead of by the csv package.
	reader.FieldsPerRecord = -1

	rows, err := reader.ReadAll()
	if err != nil {
		return ImportResult{}, fmt.Errorf("CSV okunamadı: %w", err)
	}

	if len(rows) == 0 {
		return ImportResult{}, fmt.Errorf("CSV dosyası boş")
	}

	index, err := mapColumns(rows[0])
	if err != nil {
		return ImportResult{}, err
	}

	var result ImportResult

	for i, row := range rows[1:] {
		line := i + 2 // header is line 1

		if isBlankRow(row) {
			continue
		}

		rec, err := parseRow(row, index)
		if err != nil {
			result.Errors = append(result.Errors, RowError{Line: line, Err: err})
			continue
		}

		result.Records = append(result.Records, rec)
	}

	return result, nil
}

// mapColumns locates the required columns in the header.
func mapColumns(header []string) (map[string]int, error) {
	index := make(map[string]int, len(header))

	for i, name := range header {
		index[strings.ToLower(strings.TrimSpace(name))] = i
	}

	var missing []string

	for _, required := range []string{colName, colType, colValue} {
		if _, ok := index[required]; !ok {
			missing = append(missing, required)
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf(
			"başlık satırında eksik sütun: %s (beklenen: %s)",
			strings.Join(missing, ", "), strings.Join(Header, ", "))
	}

	return index, nil
}

func parseRow(row []string, index map[string]int) (records.Record, error) {
	name, err := field(row, index, colName)
	if err != nil {
		return records.Record{}, err
	}

	typeName, err := field(row, index, colType)
	if err != nil {
		return records.Record{}, err
	}

	value, err := field(row, index, colValue)
	if err != nil {
		return records.Record{}, err
	}

	recType, err := records.ParseType(typeName)
	if err != nil {
		return records.Record{}, err
	}

	var ttl *uint32

	if raw, _ := field(row, index, colTTL); strings.TrimSpace(raw) != "" {
		parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
		if err != nil {
			return records.Record{}, fmt.Errorf("ttl %q geçersiz, saniye cinsinden bir sayı olmalı", raw)
		}

		ttl = new(uint32(parsed))
	}

	return records.New(name, recType, value, ttl)
}

func field(row []string, index map[string]int, column string) (string, error) {
	i, ok := index[column]
	if !ok || i >= len(row) {
		if column == colTTL {
			return "", nil
		}

		return "", fmt.Errorf("%s sütunu eksik", column)
	}

	value := strings.TrimSpace(row[i])
	if value == "" && column != colTTL {
		return "", fmt.Errorf("%s sütunu boş", column)
	}

	return value, nil
}

func isBlankRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}

	return true
}

// Export writes records as CSV in the same shape Import reads, so an exported
// file can be fed straight back in.
func Export(w io.Writer, recs []records.Record) error {
	writer := csv.NewWriter(w)

	if err := writer.Write(Header); err != nil {
		return fmt.Errorf("CSV başlığı yazılamadı: %w", err)
	}

	for _, rec := range recs {
		ttl := ""
		if rec.TTL != nil {
			ttl = strconv.FormatUint(uint64(*rec.TTL), 10)
		}

		row := []string{
			strings.TrimSuffix(rec.Name, "."),
			string(rec.Type),
			rec.Value,
			ttl,
		}

		if err := writer.Write(row); err != nil {
			return fmt.Errorf("CSV satırı yazılamadı: %w", err)
		}
	}

	writer.Flush()

	if err := writer.Error(); err != nil {
		return fmt.Errorf("CSV yazılamadı: %w", err)
	}

	return nil
}
