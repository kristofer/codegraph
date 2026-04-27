// Package types defines the core data types for the CodeGraph knowledge graph.
package types

import "fmt"

// =============================================================================
// ExtractionError
// =============================================================================

// Severity indicates the severity of an extraction error.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// ExtractionError is an error that occurred during code extraction.
type ExtractionError struct {
	// Message is the human-readable error description.
	Message string `json:"message"`

	// FilePath is the file where the error occurred.
	FilePath string `json:"filePath,omitempty"`

	// Line is the line number, if available.
	Line *int `json:"line,omitempty"`

	// Column is the column number, if available.
	Column *int `json:"column,omitempty"`

	// Severity is the error severity.
	Severity Severity `json:"severity"`

	// Code is an optional error code for categorization.
	Code string `json:"code,omitempty"`
}

// Error implements the error interface.
func (e *ExtractionError) Error() string {
	if e.FilePath != "" && e.Line != nil {
		return fmt.Sprintf("%s:%d: %s", e.FilePath, *e.Line, e.Message)
	}
	if e.FilePath != "" {
		return fmt.Sprintf("%s: %s", e.FilePath, e.Message)
	}
	return e.Message
}

// =============================================================================
// Sentinel errors
// =============================================================================

// ErrNotInitialized is returned when a CodeGraph project has not been initialized.
var ErrNotInitialized = fmt.Errorf("codegraph: project not initialized (run 'codegraph init')")

// ErrLanguageUnsupported is returned when a language is not supported.
var ErrLanguageUnsupported = fmt.Errorf("codegraph: language not supported")

// ErrDBNotOpen is returned when an operation is attempted on a closed database.
var ErrDBNotOpen = fmt.Errorf("codegraph: database is not open")

// ErrNodeNotFound is returned when a requested node does not exist.
var ErrNodeNotFound = fmt.Errorf("codegraph: node not found")

// ErrFileNotFound is returned when a requested file is not tracked.
var ErrFileNotFound = fmt.Errorf("codegraph: file not found")
