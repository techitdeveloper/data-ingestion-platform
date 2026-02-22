package csv

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
)

// Row represents a single parsed CSV row.
// Headers maps column name → value for every column in the file.
// This preserves all columns regardless of how many there are.
type Row struct {
	Headers map[string]string
	// LineNumber is useful for error reporting — "row 47 failed"
	LineNumber int
}

// Parser streams CSV rows from a file one at a time.
type Parser struct {
	filePath string
}

func NewParser(filePath string) *Parser {
	return &Parser{filePath: filePath}
}

// Stream opens the file and sends each row to the returned channel.
// When the file is fully read (or an error occurs), the channel is closed.
// The caller reads from the channel in a range loop — clean and idiomatic.
//
// We return two channels:
//   - rows:  the parsed row data
//   - errs:  any error that stopped parsing (buffered size 1, never blocks)
//
// This is the "generator" pattern in Go — a goroutine produces values
// into a channel, the caller consumes them. Equivalent to Elixir's Stream.resource/3.
func (p *Parser) Stream(requiredColumns []string) (<-chan Row, <-chan error) {
	rowChan := make(chan Row)
	errChan := make(chan error, 1)

	go func() {
		// Always close both channels when the goroutine exits —
		// this signals to the caller that streaming is done.
		defer close(rowChan)
		defer close(errChan)

		if err := p.stream(requiredColumns, rowChan); err != nil {
			errChan <- err
		}
	}()

	return rowChan, errChan
}

func (p *Parser) stream(requiredColumns []string, rowChan chan<- Row) error {
	f, err := os.Open(p.filePath)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	// Allow variable number of fields per row — real world CSVs are messy
	reader.FieldsPerRecord = -1

	// Read header row first
	headers, err := reader.Read()
	if err != nil {
		return fmt.Errorf("reading header row: %w", err)
	}

	// Build column name → index map from headers
	colIndex := make(map[string]int, len(headers))
	for i, h := range headers {
		colIndex[h] = i
	}

	// Validate that required columns exist
	for _, required := range requiredColumns {
		if _, ok := colIndex[required]; !ok {
			return fmt.Errorf("required column %q not found in file headers %v", required, headers)
		}
	}

	lineNumber := 1 // header was line 1
	for {
		record, err := reader.Read()
		if err != nil {
			// io.EOF means we cleanly finished the file — not an error
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("reading row at line %d: %w", lineNumber, err)
		}
		lineNumber++

		// Build header → value map for this row, covering ALL columns
		rowData := make(map[string]string, len(headers))
		for colName, idx := range colIndex {
			if idx < len(record) {
				rowData[colName] = record[idx]
			}
		}

		rowChan <- Row{
			Headers:    rowData,
			LineNumber: lineNumber,
		}
	}
}
