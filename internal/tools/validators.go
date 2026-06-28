package tools

import (
	"fmt"
	"math"
	"unicode/utf8"

	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

// checkPositive returns VALIDATION_ERROR if v <= 0.
// Mirrors z.number().positive() from the TypeScript Zod schemas.
func checkPositive(field string, v float64) *mcperr.McpError {
	if v <= 0 {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("%s must be a positive number.", field), field)
	}
	return nil
}

// checkFinite returns VALIDATION_ERROR if v is infinite or NaN.
// Mirrors z.number() (Zod rejects Infinity/NaN) from the TypeScript schemas.
func checkFinite(field string, v float64) *mcperr.McpError {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("%s must be a finite number.", field), field)
	}
	return nil
}

// checkLen returns VALIDATION_ERROR if s has fewer than min or more than max
// Unicode code points. Uses utf8.RuneCountInString rather than len(s) because
// Zod's .max() counts UTF-16 units; for the ASCII-heavy strings in this domain
// rune count is the faithful equivalent (closes S3 from Hermes review).
func checkLen(field, s string, min, max int) *mcperr.McpError {
	n := utf8.RuneCountInString(s)
	if n < min || n > max {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("%s must be between %d and %d characters.", field, min, max), field)
	}
	return nil
}

// checkMaxLen returns VALIDATION_ERROR if s exceeds max Unicode code points.
func checkMaxLen(field, s string, max int) *mcperr.McpError {
	if utf8.RuneCountInString(s) > max {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("%s must be at most %d characters.", field, max), field)
	}
	return nil
}

// checkMinOrder returns VALIDATION_ERROR if order < 0.
// Mirrors z.number().int().min(0) from the TypeScript column schema.
func checkMinOrder(field string, order int) *mcperr.McpError {
	if order < 0 {
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("%s must be >= 0.", field), field)
	}
	return nil
}
