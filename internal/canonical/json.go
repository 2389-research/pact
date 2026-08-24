// ABOUTME: Defines strict pact-json-v1 parsing, normalization, and canonical encoding.
// ABOUTME: Produces stable UTF-8 JSON bytes and SHA-256 identifiers for PACT objects.
package canonical

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const maxSafeInteger int64 = 9007199254740991

const canonicalWorkChunk = 256

type contextReader struct {
	ctx    context.Context
	reader *bytes.Reader
}

func (reader *contextReader) Read(destination []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if len(destination) > canonicalWorkChunk {
		destination = destination[:canonicalWorkChunk]
	}
	return reader.reader.Read(destination)
}

func pollContext(ctx context.Context, work *int) error {
	if *work%canonicalWorkChunk == 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	*work++
	return nil
}

// Parse reads one strict JSON value and normalizes its strings and object keys.
func Parse(raw []byte) (any, error) {
	return ParseContext(context.Background(), raw)
}

// ParseContext reads one strict JSON value while honoring cancellation during bounded work chunks.
func ParseContext(ctx context.Context, raw []byte) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return nil, fmt.Errorf("UTF-8 BOM is not allowed")
	}
	work := 0
	valid, err := validUTF8Context(ctx, raw, &work)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, fmt.Errorf("JSON is not valid UTF-8")
	}
	if err := validateSurrogateEscapesContext(ctx, raw, &work); err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(&contextReader{ctx: ctx, reader: bytes.NewReader(raw)})
	decoder.UseNumber()
	value, err := parseValueContext(ctx, decoder, &work)
	if err != nil {
		return nil, err
	}
	if err := pollContext(ctx, &work); err != nil {
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

func validUTF8Context(ctx context.Context, raw []byte, work *int) (bool, error) {
	for len(raw) != 0 {
		if err := pollContext(ctx, work); err != nil {
			return false, err
		}
		value, width := utf8.DecodeRune(raw)
		if value == utf8.RuneError && width == 1 {
			return false, nil
		}
		raw = raw[width:]
	}
	return true, nil
}

func validateSurrogateEscapesContext(ctx context.Context, raw []byte, work *int) error {
	inString := false
	for index := 0; index < len(raw); index++ {
		if err := pollContext(ctx, work); err != nil {
			return err
		}
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
	return MarshalContext(context.Background(), value)
}

// MarshalContext returns compact pact-json-v1 bytes while honoring cancellation during canonical work.
func MarshalContext(ctx context.Context, value any) ([]byte, error) {
	work := 0
	normalized, err := normalizeContext(ctx, value, "$", &work)
	if err != nil {
		return nil, err
	}
	if err := pollContext(ctx, &work); err != nil {
		return nil, err
	}
	encoded := make([]byte, 0, 128)
	return appendValueContext(ctx, encoded, normalized, &work)
}

// Digest returns the PACT SHA-256 identifier for the exact provided bytes.
func Digest(raw []byte) string {
	digest, _ := DigestContext(context.Background(), raw)
	return digest
}

// DigestContext returns the PACT SHA-256 identifier while hashing bounded chunks.
func DigestContext(ctx context.Context, raw []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	hash := sha256.New()
	for len(raw) != 0 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		end := min(canonicalWorkChunk, len(raw))
		_, _ = hash.Write(raw[:end])
		raw = raw[end:]
	}
	return "sha256:" + fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func parseValueContext(ctx context.Context, decoder *json.Decoder, work *int) (any, error) {
	if err := pollContext(ctx, work); err != nil {
		return nil, err
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	switch value := token.(type) {
	case nil, bool:
		return value, nil
	case string:
		return normalizeStringContext(ctx, value, work)
	case json.Number:
		beforeCanonicalIntegerParse()
		return parseIntegerContext(ctx, value.String(), work)
	case json.Delim:
		switch value {
		case '{':
			return parseObjectContext(ctx, decoder, work)
		case '[':
			return parseArrayContext(ctx, decoder, work)
		default:
			return nil, fmt.Errorf("invalid JSON token %q", value)
		}
	default:
		return nil, fmt.Errorf("invalid JSON value")
	}
}

func parseObjectContext(ctx context.Context, decoder *json.Decoder, work *int) (map[string]any, error) {
	if err := pollContext(ctx, work); err != nil {
		return nil, err
	}
	object := make(map[string]any)
	rawKeys := make(map[string]struct{})
	normalizedKeys := make(map[string]struct{})
	for decoder.More() {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON object key: %w", err)
		}
		rawKey, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("JSON object key is not a string")
		}
		if _, exists := rawKeys[rawKey]; exists {
			return nil, boundedObjectKeyError("duplicate JSON object key: ", rawKey)
		}
		normalizedKey, err := normalizeStringContext(ctx, rawKey, work)
		if err != nil {
			return nil, err
		}
		if _, exists := normalizedKeys[normalizedKey]; exists {
			return nil, boundedObjectKeyError("JSON object keys collide after Unicode normalization: ", rawKey)
		}
		value, err := parseValueContext(ctx, decoder, work)
		if err != nil {
			return nil, err
		}
		rawKeys[rawKey] = struct{}{}
		normalizedKeys[normalizedKey] = struct{}{}
		object[normalizedKey] = value
	}
	if err := pollContext(ctx, work); err != nil {
		return nil, err
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

const canonicalDiagnosticBytes = 512

type DiagnosticError struct {
	message   string
	truncated bool
}

func (err *DiagnosticError) Error() string { return err.message }

// DiagnosticTruncated reports whether attacker-controlled diagnostic text was clipped.
func (err *DiagnosticError) DiagnosticTruncated() bool { return err.truncated }

func boundedObjectKeyError(prefix, key string) error {
	message := make([]byte, 0, canonicalDiagnosticBytes)
	message = append(message, prefix...)
	if len(message) < canonicalDiagnosticBytes {
		message = append(message, '"')
	}
	truncated := false
keyRunes:
	for len(key) != 0 && len(message) < canonicalDiagnosticBytes-1 {
		runeValue, width := utf8.DecodeRuneInString(key)
		quotedRune := strconv.AppendQuote(nil, string(runeValue))
		escaped := quotedRune[1 : len(quotedRune)-1]
		if len(message)+len(escaped) > canonicalDiagnosticBytes-1 {
			truncated = true
			break keyRunes
		}
		message = append(message, escaped...)
		key = key[width:]
	}
	truncated = truncated || len(key) != 0
	if len(message) < canonicalDiagnosticBytes {
		message = append(message, '"')
	}
	return &DiagnosticError{message: string(message), truncated: truncated}
}

func parseArrayContext(ctx context.Context, decoder *json.Decoder, work *int) ([]any, error) {
	array := make([]any, 0)
	for decoder.More() {
		value, err := parseValueContext(ctx, decoder, work)
		if err != nil {
			return nil, err
		}
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		array = append(array, value)
	}
	if err := pollContext(ctx, work); err != nil {
		return nil, err
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

var beforeCanonicalIntegerParse = func() {}

func parseIntegerContext(ctx context.Context, value string, work *int) (int64, error) {
	for _, character := range value {
		if err := pollContext(ctx, work); err != nil {
			return 0, err
		}
		if character == '.' || character == 'e' || character == 'E' {
			return 0, fmt.Errorf("floating-point values are forbidden; use a string or scaled integer")
		}
	}
	if len(value) > len("-9007199254740991") {
		return 0, fmt.Errorf("integer outside interoperable PACT range")
	}
	integer, err := strconv.ParseInt(value, 10, 64)
	if err != nil || integer < -maxSafeInteger || integer > maxSafeInteger {
		return 0, fmt.Errorf("integer outside interoperable PACT range")
	}
	return integer, nil
}

func normalizeContext(ctx context.Context, value any, path string, work *int) (any, error) {
	if err := pollContext(ctx, work); err != nil {
		return nil, err
	}
	switch value := value.(type) {
	case []any:
		return normalizeArrayContext(ctx, value, path, work)
	case map[string]any:
		return normalizeMapContext(ctx, value, path, work)
	default:
		return normalizeScalarContext(ctx, value, path, work)
	}
}

func normalizeScalarContext(ctx context.Context, value any, path string, work *int) (any, error) {
	switch value := value.(type) {
	case nil, bool:
		return value, nil
	case string:
		valid, err := validUTF8Context(ctx, []byte(value), work)
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, fmt.Errorf("%s: string is not valid UTF-8", path)
		}
		return normalizeStringContext(ctx, value, work)
	case json.Number:
		return parseIntegerContext(ctx, value.String(), work)
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
	default:
		return nil, fmt.Errorf("%s: unsupported JSON value type %T", path, value)
	}
}

func normalizeStringContext(ctx context.Context, value string, work *int) (string, error) {
	if err := pollContext(ctx, work); err != nil {
		return "", err
	}
	normalized := make([]byte, 0, len(value))
	for len(value) != 0 {
		if err := pollContext(ctx, work); err != nil {
			return "", err
		}
		end := min(canonicalWorkChunk, len(value))
		for end < len(value) && !utf8.RuneStart(value[end]) {
			end++
		}
		normalized = norm.NFC.AppendString(normalized, value[:end])
		value = value[end:]
	}
	return string(normalized), nil
}

func normalizeArrayContext(ctx context.Context, value []any, path string, work *int) ([]any, error) {
	if err := pollContext(ctx, work); err != nil {
		return nil, err
	}
	normalized := make([]any, len(value))
	for index, item := range value {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		item, err := normalizeContext(ctx, item, fmt.Sprintf("%s[%d]", path, index), work)
		if err != nil {
			return nil, err
		}
		normalized[index] = item
	}
	return normalized, nil
}

func normalizeMapContext(ctx context.Context, value map[string]any, path string, work *int) (map[string]any, error) {
	if err := pollContext(ctx, work); err != nil {
		return nil, err
	}
	normalized := make(map[string]any, len(value))
	for rawKey, rawValue := range value {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		valid, err := validUTF8Context(ctx, []byte(rawKey), work)
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, fmt.Errorf("%s: object key is not valid UTF-8", path)
		}
		key, err := normalizeStringContext(ctx, rawKey, work)
		if err != nil {
			return nil, err
		}
		if _, exists := normalized[key]; exists {
			return nil, fmt.Errorf("%s: duplicate key after Unicode normalization: %q", path, key)
		}
		item, err := normalizeContext(ctx, rawValue, path+"."+key, work)
		if err != nil {
			return nil, err
		}
		normalized[key] = item
	}
	return normalized, nil
}

func normalizeInteger(value int64, path string) (int64, error) {
	if value < -maxSafeInteger || value > maxSafeInteger {
		return 0, fmt.Errorf("%s: integer outside interoperable PACT range", path)
	}
	return value, nil
}

func appendValueContext(ctx context.Context, dst []byte, value any, work *int) ([]byte, error) {
	if err := pollContext(ctx, work); err != nil {
		return nil, err
	}
	switch value := value.(type) {
	case nil:
		return append(dst, "null"...), nil
	case bool:
		return strconv.AppendBool(dst, value), nil
	case int64:
		return strconv.AppendInt(dst, value, 10), nil
	case string:
		return appendJSONStringContext(ctx, dst, value, work)
	case []any:
		return appendJSONArrayContext(ctx, dst, value, work)
	case map[string]any:
		return appendJSONObjectContext(ctx, dst, value, work)
	default:
		panic("canonical: unnormalized value")
	}
}

func appendJSONArrayContext(ctx context.Context, dst []byte, value []any, work *int) ([]byte, error) {
	dst = append(dst, '[')
	for index, item := range value {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		if index > 0 {
			dst = append(dst, ',')
		}
		var err error
		dst, err = appendValueContext(ctx, dst, item, work)
		if err != nil {
			return nil, err
		}
	}
	return append(dst, ']'), nil
}

func appendJSONObjectContext(ctx context.Context, dst []byte, value map[string]any, work *int) ([]byte, error) {
	keys, err := sortedMapKeysContext(ctx, value, work)
	if err != nil {
		return nil, err
	}
	dst = append(dst, '{')
	for index, key := range keys {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		if index > 0 {
			dst = append(dst, ',')
		}
		dst, err = appendJSONStringContext(ctx, dst, key, work)
		if err != nil {
			return nil, err
		}
		dst = append(dst, ':')
		dst, err = appendValueContext(ctx, dst, value[key], work)
		if err != nil {
			return nil, err
		}
	}
	return append(dst, '}'), nil
}

func sortedMapKeysContext(ctx context.Context, value map[string]any, work *int) ([]string, error) {
	if err := pollContext(ctx, work); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(value))
	for key := range value {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	for start := 0; start < len(keys); start += canonicalWorkChunk {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
		end := min(start+canonicalWorkChunk, len(keys))
		sort.Strings(keys[start:end])
	}
	if len(keys) <= canonicalWorkChunk {
		return keys, nil
	}
	if err := pollContext(ctx, work); err != nil {
		return nil, err
	}
	buffer := make([]string, len(keys))
	for width := canonicalWorkChunk; width < len(keys); width *= 2 {
		for start := 0; start < len(keys); start += 2 * width {
			middle := min(start+width, len(keys))
			end := min(start+2*width, len(keys))
			left, right := start, middle
			for output := start; output < end; output++ {
				if err := pollContext(ctx, work); err != nil {
					return nil, err
				}
				if right >= end || left < middle && keys[left] <= keys[right] {
					buffer[output] = keys[left]
					left++
				} else {
					buffer[output] = keys[right]
					right++
				}
			}
		}
		keys, buffer = buffer, keys
	}
	return keys, nil
}

func appendJSONStringContext(ctx context.Context, dst []byte, value string, work *int) ([]byte, error) {
	dst = append(dst, '"')
	for _, character := range value {
		if err := pollContext(ctx, work); err != nil {
			return nil, err
		}
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
			dst = utf8.AppendRune(dst, character)
		}
	}
	return append(dst, '"'), nil
}
