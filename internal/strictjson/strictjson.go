// Package strictjson makes encoding/json's decoding of Cedar JSON objects case-sensitive.
//
// encoding/json binds an object key to a struct field whose name matches it case-insensitively, so
// {"TYPE": "User"} populates a field tagged `json:"type"`, and DisallowUnknownFields still treats
// such keys as known. Cedar's JSON formats are case-sensitive: the Rust implementation does not
// treat "TYPE" as "type". Until encoding/json/v2, which matches names exactly, can be used, the
// helpers here check an object's top-level keys against the exact JSON field names of a struct.
//
// The checks are not recursive. Each struct type that needs them calls them from its own
// UnmarshalJSON method, and encoding/json invokes those methods for nested values.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"
)

// ErrMiscasedKey is returned by Unmarshal when an object key differs from a field name only by case.
var ErrMiscasedKey = errors.New("json key does not match the case of the field name")

// Unmarshal is json.Unmarshal for v, a pointer to a struct, except that it returns an error wrapping
// ErrMiscasedKey if a top-level key of data matches one of v's JSON field names only
// case-insensitively. Keys that match no field are left to json.Unmarshal, which ignores them.
//
// A miscased key is rejected rather than ignored like other unknown keys: ignoring it would silently
// drop data, e.g. the attributes under "ATTRS", where the input was most likely meant for the field.
func Unmarshal(data []byte, v any) error {
	f := fieldsOf(v)
	var err error
	forEachKey(data, func(key []byte) bool {
		if _, ok := f.names[string(key)]; ok {
			return true
		}
		for _, name := range f.list {
			// encoding/json folds names with bytes.EqualFold, so this finds exactly the keys it would
			// have bound to a field.
			if bytes.EqualFold(key, name) {
				err = fmt.Errorf("%w: %q (want %q)", ErrMiscasedKey, key, name)
				return false
			}
		}
		return true
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// HasUnknownKeys reports whether data is a JSON object with a top-level key that is not exactly one
// of the JSON field names of v, a pointer to a struct. It is a case-sensitive replacement for
// decoding with json.Decoder.DisallowUnknownFields and checking for an unknown field error, and it
// only looks at this level, so errors from nested values cannot be mistaken for one.
func HasUnknownKeys(data []byte, v any) bool {
	f := fieldsOf(v)
	unknown := false
	forEachKey(data, func(key []byte) bool {
		_, ok := f.names[string(key)]
		unknown = !ok
		return ok
	})
	return unknown
}

// forEachKey calls fn with each top-level key of data, if data is a JSON object, until fn returns
// false. It is a single pass that skips over values without decoding them.
//
// forEachKey does not validate data; the caller passes data to json.Unmarshal afterwards, which
// does. It stops early at anything that is not shaped like a JSON object, leaving json.Unmarshal to
// report the problem in terms of the caller's type.
func forEachKey(data []byte, fn func(key []byte) bool) {
	i := skipSpace(data, 0)
	if i == len(data) || data[i] != '{' {
		return
	}
	for i++; ; i++ {
		i = skipSpace(data, i)
		if i == len(data) || data[i] != '"' {
			return // the end of the object, or malformed
		}
		end := skipString(data, i)
		if end < 0 {
			return
		}
		key := data[i+1 : end-1]
		if bytes.IndexByte(key, '\\') >= 0 || !utf8.Valid(key) {
			// Escape sequences and invalid UTF-8 are rare in keys; let encoding/json decode them
			// exactly as it would.
			var s string
			if json.Unmarshal(data[i:end], &s) != nil {
				return
			}
			key = []byte(s)
		}
		if !fn(key) {
			return
		}
		i = skipSpace(data, end)
		if i == len(data) || data[i] != ':' {
			return
		}
		i = skipValue(data, i+1)
		if i == len(data) || data[i] != ',' {
			return
		}
	}
}

func skipSpace(data []byte, i int) int {
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
		i++
	}
	return i
}

// skipString returns the index just past the end of the string that starts with the quote at
// data[i], or -1 if the string is not terminated.
func skipString(data []byte, i int) int {
	for i++; i < len(data); i++ {
		switch data[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return -1
}

// skipValue returns the index of the ',' or closing bracket that ends the value starting at
// data[i], or len(data) if there is none.
func skipValue(data []byte, i int) int {
	depth := 0
	for i < len(data) {
		switch data[i] {
		case '"':
			if i = skipString(data, i); i < 0 {
				return len(data)
			}
			continue
		case '{', '[':
			depth++
		case '}', ']':
			if depth == 0 {
				return i
			}
			depth--
		case ',':
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return i
}

// fields holds the JSON names of a struct's fields.
type fields struct {
	names map[string]struct{}
	list  [][]byte
}

var fieldsCache sync.Map // map[reflect.Type]*fields

// fieldsOf returns the JSON names of the fields of the struct v points to, computed the way
// encoding/json does for the flat structs this package is used with.
func fieldsOf(v any) *fields {
	t := reflect.TypeOf(v).Elem()
	if f, ok := fieldsCache.Load(t); ok {
		return f.(*fields)
	}
	f := &fields{names: map[string]struct{}{}}
	for i := range t.NumField() {
		sf := t.Field(i)
		if sf.Anonymous {
			// encoding/json promotes the fields of embedded structs; supporting that is not needed.
			panic(fmt.Sprintf("strictjson: embedded field %s in %s is not supported", sf.Name, t))
		}
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = sf.Name
		}
		f.names[name] = struct{}{}
		f.list = append(f.list, []byte(name))
	}
	fieldsCache.Store(t, f)
	return f
}
