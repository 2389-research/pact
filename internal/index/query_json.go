// ABOUTME: Streams the one QueryPage JSON representation with its exact trailing newline.
// ABOUTME: Enforces the Phase 2 byte limit without allocating a full encoded result buffer.
package index

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"pact/internal/ledger"
)

var errQueryJSONLimit = errors.New("query JSON exceeds limit")

type boundedQueryJSONWriter struct {
	destination io.Writer
	maximum     uint64
	written     uint64
}

func (writer *boundedQueryJSONWriter) Write(value []byte) (int, error) {
	if writer.written > writer.maximum || uint64(len(value)) > writer.maximum-writer.written {
		return 0, errQueryJSONLimit
	}
	written, err := writer.destination.Write(value)
	// #nosec G115 -- io.Writer's contract keeps written between zero and len(value).
	writer.written += uint64(written)
	if err == nil && written != len(value) {
		return written, io.ErrShortWrite
	}
	return written, err
}

// WriteQueryPageJSON writes the exact bounded JSON line emitted by log and query.
func WriteQueryPageJSON(ctx context.Context, destination io.Writer, page QueryPage) error {
	return writeQueryPageJSONLimit(ctx, destination, page, ledger.Phase2Limits.JSONResultBytes)
}

func writeQueryPageJSONLimit(ctx context.Context, destination io.Writer, page QueryPage, maximum uint64) error {
	if ctx == nil || destination == nil {
		return fmt.Errorf("context and JSON destination are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	writer := &boundedQueryJSONWriter{destination: destination, maximum: maximum}
	encoder := queryJSONEncoder{ctx: ctx, writer: writer}
	if err := encoder.write(reflect.ValueOf(page)); err != nil {
		return queryJSONError(err, maximum)
	}
	if _, err := writer.Write([]byte{'\n'}); err != nil {
		return queryJSONError(err, maximum)
	}
	return ctx.Err()
}

func queryPageJSONSizeLimit(ctx context.Context, page QueryPage, maximum uint64) (uint64, error) {
	writer := &boundedQueryJSONWriter{destination: io.Discard, maximum: maximum}
	if err := writeQueryPageJSONLimit(ctx, writer, page, maximum); err != nil {
		return 0, err
	}
	return writer.written, nil
}

func queryJSONError(err error, maximum uint64) error {
	if errors.Is(err, errQueryJSONLimit) {
		return &ledger.LimitError{Resource: "json_result_bytes", Maximum: maximum, ObservedAtLeast: maximum + 1}
	}
	return err
}

func queryJSONValueSizeLimit(ctx context.Context, value any, maximum uint64) (uint64, error) {
	if ctx == nil {
		return 0, fmt.Errorf("context is required")
	}
	writer := &boundedQueryJSONWriter{destination: io.Discard, maximum: maximum}
	encoder := queryJSONEncoder{ctx: ctx, writer: writer}
	if err := encoder.write(reflect.ValueOf(value)); err != nil {
		return 0, queryJSONError(err, maximum)
	}
	return writer.written, ctx.Err()
}

type queryJSONEncoder struct {
	ctx    context.Context
	writer io.Writer
	work   int
}

func (encoder *queryJSONEncoder) write(value reflect.Value) error { //nolint:gocyclo // The fixed JSON kind switch mirrors encoding/json without a page buffer.
	if err := encoder.poll(); err != nil {
		return err
	}
	if !value.IsValid() {
		return encoder.raw("null")
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return encoder.raw("null")
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Bool:
		return encoder.raw(strconv.FormatBool(value.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return encoder.raw(strconv.FormatInt(value.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return encoder.raw(strconv.FormatUint(value.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		bits := value.Type().Bits()
		number := value.Float()
		if math.IsInf(number, 0) || math.IsNaN(number) {
			return fmt.Errorf("unsupported JSON number")
		}
		return encoder.raw(strconv.FormatFloat(number, 'g', -1, bits))
	case reflect.String:
		return encoder.string(value.String())
	case reflect.Struct:
		return encoder.structure(value)
	case reflect.Slice:
		if value.IsNil() {
			return encoder.raw("null")
		}
		return encoder.sequence(value)
	case reflect.Array:
		return encoder.sequence(value)
	default:
		return fmt.Errorf("unsupported query JSON type %s", value.Type())
	}
}

func (encoder *queryJSONEncoder) structure(value reflect.Value) error {
	if err := encoder.raw("{"); err != nil {
		return err
	}
	wrote := false
	typeOfValue := value.Type()
	for index := range value.NumField() {
		fieldType := typeOfValue.Field(index)
		if fieldType.PkgPath != "" {
			continue
		}
		name, omitEmpty, skip := queryJSONField(fieldType)
		if skip || omitEmpty && queryJSONEmpty(value.Field(index)) {
			continue
		}
		if wrote {
			if err := encoder.raw(","); err != nil {
				return err
			}
		}
		wrote = true
		if err := encoder.string(name); err != nil {
			return err
		}
		if err := encoder.raw(":"); err != nil {
			return err
		}
		if err := encoder.write(value.Field(index)); err != nil {
			return err
		}
	}
	return encoder.raw("}")
}

func queryJSONField(field reflect.StructField) (string, bool, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = field.Name
	}
	omitEmpty := false
	for _, option := range parts[1:] {
		omitEmpty = omitEmpty || option == "omitempty"
	}
	return name, omitEmpty, false
}

func queryJSONEmpty(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return value.Len() == 0
	case reflect.Bool:
		return !value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return value.IsNil()
	}
	return false
}

func (encoder *queryJSONEncoder) sequence(value reflect.Value) error {
	if err := encoder.raw("["); err != nil {
		return err
	}
	for index := range value.Len() {
		if index != 0 {
			if err := encoder.raw(","); err != nil {
				return err
			}
		}
		if err := encoder.write(value.Index(index)); err != nil {
			return err
		}
	}
	return encoder.raw("]")
}

func (encoder *queryJSONEncoder) string(value string) error { //nolint:funlen,gocognit,gocyclo // Streaming encoding keeps the standard JSON escape cases explicit without a full string buffer.
	if err := encoder.raw("\""); err != nil {
		return err
	}
	buffer := make([]byte, 0, 4_096)
	flush := func() error {
		if len(buffer) == 0 {
			return nil
		}
		_, err := encoder.writer.Write(buffer)
		buffer = buffer[:0]
		return err
	}
	appendBytes := func(raw ...byte) error {
		if len(buffer)+len(raw) > cap(buffer) {
			if err := flush(); err != nil {
				return err
			}
		}
		buffer = append(buffer, raw...)
		return nil
	}
	nextPoll := uint64(0)
	for index := 0; index < len(value); {
		// #nosec G115 -- a string index is nonnegative and cannot exceed uint64.
		if uint64(index) >= nextPoll {
			if err := encoder.ctx.Err(); err != nil {
				return err
			}
			nextPoll += 256
		}
		character := value[index]
		if character < utf8.RuneSelf { //nolint:nestif // The standard ASCII escape table is clearer as one bounded branch.
			index++
			switch character {
			case '\\', '"':
				if err := appendBytes('\\', character); err != nil {
					return err
				}
			case '\b':
				if err := appendBytes('\\', 'b'); err != nil {
					return err
				}
			case '\f':
				if err := appendBytes('\\', 'f'); err != nil {
					return err
				}
			case '\n':
				if err := appendBytes('\\', 'n'); err != nil {
					return err
				}
			case '\r':
				if err := appendBytes('\\', 'r'); err != nil {
					return err
				}
			case '\t':
				if err := appendBytes('\\', 't'); err != nil {
					return err
				}
			case '<', '>', '&':
				if err := appendBytes('\\', 'u', '0', '0', hexDigit(character>>4), hexDigit(character)); err != nil {
					return err
				}
			default:
				if character < 0x20 {
					if err := appendBytes('\\', 'u', '0', '0', hexDigit(character>>4), hexDigit(character)); err != nil {
						return err
					}
				} else if err := appendBytes(character); err != nil {
					return err
				}
			}
			continue
		}
		runeValue, size := utf8.DecodeRuneInString(value[index:])
		if runeValue == utf8.RuneError && size == 1 {
			if err := appendBytes('\\', 'u', 'f', 'f', 'f', 'd'); err != nil {
				return err
			}
			index++
			continue
		}
		if runeValue == '\u2028' || runeValue == '\u2029' {
			lastDigit := byte('8')
			if runeValue == '\u2029' {
				lastDigit = '9'
			}
			if err := appendBytes('\\', 'u', '2', '0', '2', lastDigit); err != nil {
				return err
			}
		} else if err := appendBytes([]byte(value[index : index+size])...); err != nil {
			return err
		}
		index += size
	}
	if err := flush(); err != nil {
		return err
	}
	return encoder.raw("\"")
}

func hexDigit(value byte) byte {
	const digits = "0123456789abcdef"
	return digits[value&0xf]
}

func (encoder *queryJSONEncoder) raw(value string) error {
	_, err := io.WriteString(encoder.writer, value)
	return err
}

func (encoder *queryJSONEncoder) poll() error {
	err := pollIndexContext(encoder.ctx, encoder.work)
	encoder.work++
	return err
}
