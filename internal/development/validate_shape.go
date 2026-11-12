// ABOUTME: Checks shapes from the profile's shared closed field definitions.
// ABOUTME: Supports only the fixed vocabulary's field forms, without loading caller schemas.
package development

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"unicode/utf8"
)

func validateShape(ctx context.Context, value any, shape map[string]any, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch shape["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok || object == nil {
			return invalid(path, "shape", "expected one object with the required fields")
		}
		return validateObjectShape(ctx, object, shape, path)
	case "array":
		values, ok := value.([]any)
		if !ok {
			return invalid(path, "shape", "expected an array, including when empty")
		}
		return validateArrayShape(ctx, values, shape, path)
	case "string":
		text, ok := value.(string)
		if !ok {
			return invalid(path, "shape", "expected a string")
		}
		return validateTextShape(text, shape, path)
	case "integer":
		number, ok := value.(int64)
		if !ok {
			return invalid(path, "shape", "expected a whole safe integer")
		}
		if minimum, exists := shape["minimum"].(int); exists && number < int64(minimum) {
			return invalid(path, "bounds", "integer is below the minimum")
		}
		return nil
	default:
		return invalid(path, "profile", "unsupported bundled field definition")
	}
}

func validateObjectShape(ctx context.Context, object, shape map[string]any, path string) error {
	properties, closed := shape["properties"].(map[string]any)
	if !closed {
		return nil
	} // The record's payload and evidence objects have core/domain owners.
	if additional, exists := shape["additionalProperties"].(bool); exists && !additional {
		for name := range object {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, known := properties[name]; !known {
				return invalid(path, "unknown_field", "object contains a field outside the fixed contract")
			}
		}
	}
	required, _ := shape["required"].([]string)
	for _, name := range required {
		if _, exists := object[name]; !exists {
			return invalid(path+"."+name, "required", "required field is missing")
		}
	}
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		child, exists := object[name]
		if !exists {
			continue
		}
		field, _ := properties[name].(map[string]any)
		if err := validateShape(ctx, child, field, path+"."+name); err != nil {
			return err
		}
	}
	return nil
}

func validateArrayShape(ctx context.Context, values []any, shape map[string]any, path string) error {
	minimum, _ := shape["minItems"].(int)
	if len(values) < minimum {
		return invalid(path, "required", "array has too few entries")
	}
	itemShape, _ := shape["items"].(map[string]any)
	unique, _ := shape["uniqueItems"].(bool)
	seen := map[int64]bool{}
	for index, item := range values {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		if err := validateShape(ctx, item, itemShape, itemPath); err != nil {
			return err
		}
		// Only evidence-index arrays use uniqueItems in the fixed definitions.
		if unique {
			number, ok := item.(int64)
			if !ok {
				return invalid(itemPath, "profile", "unique entries must be evidence indexes")
			}
			if seen[number] {
				return invalid(itemPath, "duplicate", "evidence indexes must be distinct")
			}
			seen[number] = true
		}
	}
	return nil
}

func validateTextShape(text string, shape map[string]any, path string) error {
	minimum, _ := shape["minLength"].(int)
	if utf8.RuneCountInString(text) < minimum {
		return invalid(path, "required", "string is too short")
	}
	if values, exists := shape["enum"].([]string); exists && !slices.Contains(values, text) {
		return invalid(path, "enum", "expected a supported profile value")
	}
	if pattern, exists := shape["pattern"].(string); exists {
		matched, err := regexp.MatchString(pattern, text)
		if err != nil || !matched {
			return invalid(path, "format", "string does not match the fixed identity format")
		}
	}
	if excluded, exists := shape["not"].(map[string]any); exists {
		pattern, _ := excluded["pattern"].(string)
		matched, err := regexp.MatchString(pattern, text)
		if err != nil || matched {
			return invalid(path, "format", "identity cannot contain whitespace")
		}
	}
	return nil
}
