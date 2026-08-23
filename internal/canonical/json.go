// ABOUTME: Defines strict pact-json-v1 parsing, normalization, and canonical encoding.
// ABOUTME: Produces stable UTF-8 JSON bytes and SHA-256 identifiers for PACT objects.
package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const maxSafeInteger int64 = 9007199254740991

// Parse reads one strict JSON value and normalizes its strings and object keys.
func Parse(raw []byte) (any, error) {
	if bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return nil, fmt.Errorf("UTF-8 BOM is not allowed")
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("JSON is not valid UTF-8")
	}
	if err := validateSurrogateEscapes(raw); err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := parseValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing data after JSON value")
		}
		return nil, fmt.Errorf("invalid JSON after value: %w", err)
	}
	return value, nil
}

func validateSurrogateEscapes(raw []byte) error {
	inString := false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || index+1 >= len(raw) {
				continue
			}
			if raw[index+1] != 'u' {
				index++
				continue
			}
			surrogate, ok := escapedUTF16(raw, index+2)
			if !ok {
				continue
			}
			if surrogate >= 0xd800 && surrogate <= 0xdbff {
				lowSurrogate, ok := escapedUTF16(raw, index+8)
				if !ok || raw[index+6] != '\\' || raw[index+7] != 'u' || lowSurrogate < 0xdc00 || lowSurrogate > 0xdfff {
					return fmt.Errorf("unpaired high surrogate escape")
				}
				index += 11
				continue
			}
			if surrogate >= 0xdc00 && surrogate <= 0xdfff {
				return fmt.Errorf("unpaired low surrogate escape")
			}
			index += 5
		}
	}
	return nil
}

func escapedUTF16(raw []byte, start int) (uint16, bool) {
	if start+4 > len(raw) {
		return 0, false
	}
	var value uint16
	for _, digit := range raw[start : start+4] {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value |= uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

// Marshal returns the compact pact-json-v1 encoding of a JSON-compatible value.
func Marshal(value any) ([]byte, error) {
	normalized, err := normalize(value, "$")
	if err != nil {
		return nil, err
	}
	encoded := make([]byte, 0, 128)
	return appendValue(encoded, normalized), nil
}

// Digest returns the PACT SHA-256 identifier for the exact provided bytes.
func Digest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + fmt.Sprintf("%x", digest)
}

func parseValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	switch value := token.(type) {
	case nil, bool:
		return value, nil
	case string:
		return norm.NFC.String(value), nil
	case json.Number:
		return parseInteger(value.String())
	case json.Delim:
		switch value {
		case '{':
			return parseObject(decoder)
		case '[':
			return parseArray(decoder)
		default:
			return nil, fmt.Errorf("invalid JSON token %q", value)
		}
	default:
		return nil, fmt.Errorf("invalid JSON value")
	}
}

func parseObject(decoder *json.Decoder) (map[string]any, error) {
	object := make(map[string]any)
	rawKeys := make(map[string]struct{})
	normalizedKeys := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON object key: %w", err)
		}
		rawKey, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("JSON object key is not a string")
		}
		if _, exists := rawKeys[rawKey]; exists {
			return nil, fmt.Errorf("duplicate JSON object key: %q", rawKey)
		}
		normalizedKey := norm.NFC.String(rawKey)
		if _, exists := normalizedKeys[normalizedKey]; exists {
			return nil, fmt.Errorf("JSON object keys collide after Unicode normalization: %q", rawKey)
		}
		value, err := parseValue(decoder)
		if err != nil {
			return nil, err
		}
		rawKeys[rawKey] = struct{}{}
		normalizedKeys[normalizedKey] = struct{}{}
		object[normalizedKey] = value
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}
	if closing != json.Delim('}') {
		return nil, fmt.Errorf("invalid JSON object")
	}
	return object, nil
}

func parseArray(decoder *json.Decoder) ([]any, error) {
	array := make([]any, 0)
	for decoder.More() {
		value, err := parseValue(decoder)
		if err != nil {
			return nil, err
		}
		array = append(array, value)
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON array: %w", err)
	}
	if closing != json.Delim(']') {
		return nil, fmt.Errorf("invalid JSON array")
	}
	return array, nil
}

func parseInteger(value string) (int64, error) {
	if strings.ContainsAny(value, ".eE") {
		return 0, fmt.Errorf("floating-point values are forbidden; use a string or scaled integer")
	}
	integer, err := strconv.ParseInt(value, 10, 64)
	if err != nil || integer < -maxSafeInteger || integer > maxSafeInteger {
		return 0, fmt.Errorf("integer outside interoperable PACT range")
	}
	return integer, nil
}

func normalize(value any, path string) (any, error) {
	switch value := value.(type) {
	case nil, bool:
		return value, nil
	case string:
		if !utf8.ValidString(value) {
			return nil, fmt.Errorf("%s: string is not valid UTF-8", path)
		}
		return norm.NFC.String(value), nil
	case json.Number:
		return parseInteger(value.String())
	case int:
		return normalizeInteger(int64(value), path)
	case int8:
		return normalizeInteger(int64(value), path)
	case int16:
		return normalizeInteger(int64(value), path)
	case int32:
		return normalizeInteger(int64(value), path)
	case int64:
		return normalizeInteger(value, path)
	case uint:
		if uint64(value) > uint64(maxSafeInteger) {
			return nil, fmt.Errorf("%s: integer outside interoperable PACT range", path)
		}
		return int64(value), nil
	case uint8:
		return int64(value), nil
	case uint16:
		return int64(value), nil
	case uint32:
		return int64(value), nil
	case uint64:
		if value > uint64(maxSafeInteger) {
			return nil, fmt.Errorf("%s: integer outside interoperable PACT range", path)
		}
		return int64(value), nil
	case float32, float64:
		return nil, fmt.Errorf("%s: floating-point values are forbidden; use a string or scaled integer", path)
	case []any:
		normalized := make([]any, len(value))
		for index, item := range value {
			item, err := normalize(item, fmt.Sprintf("%s[%d]", path, index))
			if err != nil {
				return nil, err
			}
			normalized[index] = item
		}
		return normalized, nil
	case map[string]any:
		normalized := make(map[string]any, len(value))
		for rawKey, rawValue := range value {
			if !utf8.ValidString(rawKey) {
				return nil, fmt.Errorf("%s: object key is not valid UTF-8", path)
			}
			key := norm.NFC.String(rawKey)
			if _, exists := normalized[key]; exists {
				return nil, fmt.Errorf("%s: duplicate key after Unicode normalization: %q", path, key)
			}
			item, err := normalize(rawValue, path+"."+key)
			if err != nil {
				return nil, err
			}
			normalized[key] = item
		}
		return normalized, nil
	default:
		return nil, fmt.Errorf("%s: unsupported JSON value type %T", path, value)
	}
}

func normalizeInteger(value int64, path string) (int64, error) {
	if value < -maxSafeInteger || value > maxSafeInteger {
		return 0, fmt.Errorf("%s: integer outside interoperable PACT range", path)
	}
	return value, nil
}

func appendValue(dst []byte, value any) []byte {
	switch value := value.(type) {
	case nil:
		return append(dst, "null"...)
	case bool:
		return strconv.AppendBool(dst, value)
	case int64:
		return strconv.AppendInt(dst, value, 10)
	case string:
		return appendJSONString(dst, value)
	case []any:
		dst = append(dst, '[')
		for index, item := range value {
			if index > 0 {
				dst = append(dst, ',')
			}
			dst = appendValue(dst, item)
		}
		return append(dst, ']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		dst = append(dst, '{')
		for index, key := range keys {
			if index > 0 {
				dst = append(dst, ',')
			}
			dst = appendJSONString(dst, key)
			dst = append(dst, ':')
			dst = appendValue(dst, value[key])
		}
		return append(dst, '}')
	default:
		panic("canonical: unnormalized value")
	}
}

func appendJSONString(dst []byte, value string) []byte {
	dst = append(dst, '"')
	for _, character := range value {
		switch character {
		case '"':
			dst = append(dst, '\\', '"')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			if character < 0x20 {
				dst = append(dst, '\\', 'u', '0', '0', "0123456789abcdef"[character>>4], "0123456789abcdef"[character&0xf])
				continue
			}
			dst = append(dst, string(character)...)
		}
	}
	return append(dst, '"')
}
