package sliceprotocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

func Decode(reader io.Reader) (Envelope, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, MaxPayloadBytes+1))
	if err != nil {
		return Envelope{}, fmt.Errorf("read protocol payload: %w", err)
	}
	if len(payload) > MaxPayloadBytes {
		return Envelope{}, fmt.Errorf("%w: payload too large", ErrInvalid)
	}
	if err := RejectDuplicateKeys(payload); err != nil {
		return Envelope{}, err
	}
	var envelope Envelope
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, fmt.Errorf("%w: decode: %v", ErrInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Envelope{}, fmt.Errorf("%w: trailing JSON", ErrInvalid)
	}
	if err := ValidateEnvelope(envelope); err != nil {
		return Envelope{}, err
	}
	if envelope.Authoritative != nil {
		canonical := Canonicalize(*envelope.Authoritative)
		envelope.Authoritative = &canonical
	}
	envelope.Observation.DegradedReasons = SortReasons(envelope.Observation.DegradedReasons)
	return envelope, nil
}

func Encode(writer io.Writer, envelope Envelope) error {
	if envelope.Authoritative != nil {
		canonical := Canonicalize(*envelope.Authoritative)
		envelope.Authoritative = &canonical
	}
	envelope.Observation.DegradedReasons = SortReasons(envelope.Observation.DegradedReasons)
	if err := ValidateEnvelope(envelope); err != nil {
		return err
	}
	buffer := &boundedEncodeBuffer{max: MaxPayloadBytes}
	encoder := json.NewEncoder(buffer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(envelope); err != nil {
		if errors.Is(err, errEncodedPayloadTooLarge) {
			return fmt.Errorf("%w: encoded payload too large", ErrInvalid)
		}
		return err
	}
	written, err := writer.Write(buffer.Bytes())
	if err != nil {
		return err
	}
	if written != buffer.Len() {
		return io.ErrShortWrite
	}
	return nil
}

var errEncodedPayloadTooLarge = errors.New("encoded payload too large")

type boundedEncodeBuffer struct {
	bytes.Buffer
	max int
}

func (buffer *boundedEncodeBuffer) Write(payload []byte) (int, error) {
	if buffer.Len()+len(payload) > buffer.max {
		return 0, errEncodedPayloadTooLarge
	}
	return buffer.Buffer.Write(payload)
}

// RejectDuplicateKeys validates one JSON value and rejects ambiguous duplicate object keys.
func RejectDuplicateKeys(payload []byte) error {
	if !utf8.Valid(payload) {
		return fmt.Errorf("%w: payload is not valid UTF-8", ErrInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := scanValue(decoder); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON", ErrInvalid)
	}
	return nil
}

// RejectUnknownFieldsExact rejects object keys that do not exactly match the
// target's JSON field tags. encoding/json otherwise accepts case-insensitive
// spellings even with DisallowUnknownFields, which is unsuitable at hostile
// protocol and persisted-authority boundaries.
func RejectUnknownFieldsExact(payload []byte, target any) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("%w: decode field shape: %v", ErrInvalid, err)
	}
	typ := reflect.TypeOf(target)
	if typ == nil {
		return fmt.Errorf("%w: missing field-shape target", ErrInvalid)
	}
	if err := rejectUnknownShape(value, typ); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func rejectUnknownShape(value any, typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == rawMessageType || typ.Kind() == reflect.Interface {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return nil // The typed decoder reports kind mismatches.
		}
		fields := make(map[string]reflect.Type)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			fields[name] = field.Type
		}
		for key, child := range object {
			fieldType, found := fields[key]
			if !found {
				return fmt.Errorf("unknown object key %q", key)
			}
			if err := rejectUnknownShape(child, fieldType); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		array, ok := value.([]any)
		if !ok {
			return nil
		}
		for _, child := range array {
			if err := rejectUnknownShape(child, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		for _, child := range object {
			if err := rejectUnknownShape(child, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func scanValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, found := seen[key]; found {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("unterminated object")
		}
	case '[':
		for decoder.More() {
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("unterminated array")
		}
	default:
		return fmt.Errorf("unexpected delimiter")
	}
	return nil
}
