package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cedar-policy/cedar-go/internal/strictjson"
)

var (
	errJSONInvalidExtn     = fmt.Errorf("invalid extension")
	errJSONDecode          = fmt.Errorf("error decoding json")
	errJSONLongOutOfRange  = fmt.Errorf("long out of range")
	errJSONUnsupportedType = fmt.Errorf("unsupported type")
	errJSONExtFnMatch      = fmt.Errorf("json extn mismatch")
	errJSONExtNotFound     = fmt.Errorf("json extn not found")
	errJSONEntityNotFound  = fmt.Errorf("json entity not found")
)

type extn struct {
	Fn  string `json:"fn"`
	Arg string `json:"arg"`
}

type extValueJSON struct {
	Extn *extn `json:"__extn,omitempty"`
}

type extEntity struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type entityValueJSON struct {
	Type   *string    `json:"type,omitempty"`
	ID     *string    `json:"id,omitempty"`
	Entity *extEntity `json:"__entity,omitempty"`
}

// The escape objects above are matched case-sensitively, as in the Rust implementation. A miscased
// key makes decoding fail, so UnmarshalJSON falls back to treating the object as a Record, like Rust.

func (e *extn) UnmarshalJSON(b []byte) error {
	type extnAlias extn
	return strictjson.Unmarshal(b, (*extnAlias)(e))
}

func (e *extValueJSON) UnmarshalJSON(b []byte) error {
	type extValueJSONAlias extValueJSON
	return strictjson.Unmarshal(b, (*extValueJSONAlias)(e))
}

func (e *extEntity) UnmarshalJSON(b []byte) error {
	type extEntityAlias extEntity
	return strictjson.Unmarshal(b, (*extEntityAlias)(e))
}

func (e *entityValueJSON) UnmarshalJSON(b []byte) error {
	type entityValueJSONAlias entityValueJSON
	return strictjson.Unmarshal(b, (*entityValueJSONAlias)(e))
}

type explicitValue struct {
	Value Value
}

func UnmarshalJSON(b []byte, v *Value) error {
	// TODO: make this faster if it matters
	// Only an object can be an extension escape; skipping the attempt for other values keeps the
	// escape's case-sensitive key check off the path for strings, numbers, booleans, and sets.
	if trimmed := bytes.TrimLeft(b, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '{' {
		var res extValueJSON
		if err := json.Unmarshal(b, &res); err == nil && res.Extn != nil {
			switch res.Extn.Fn {
			case "ip":
				val, err := ParseIPAddr(res.Extn.Arg)
				if err != nil {
					return err
				}
				*v = val
				return nil
			case "decimal":
				val, err := ParseDecimal(res.Extn.Arg)
				if err != nil {
					return err
				}
				*v = val
				return nil
			case "datetime":
				val, err := ParseDatetime(res.Extn.Arg)
				if err != nil {
					return err
				}
				*v = val
				return nil
			case "duration":
				val, err := ParseDuration(res.Extn.Arg)
				if err != nil {
					return err
				}
				*v = val
				return nil
			default:
				return errJSONInvalidExtn
			}
		}
	}

	if len(b) > 0 {
		switch b[0] {
		case '[':
			var res Set
			err := json.Unmarshal(b, &res)
			*v = res
			return err
		case '{':
			// Try explicit EntityUID form {"__entity": {...}} before Record.
			// Only the explicit escape is attempted here; the implicit form
			// {"type":"X","id":"Y"} parses as a Record and requires
			// schema-guided coercion (see x/exp/types.EntityMap.UnmarshalJSONWithSchema).
			var ej entityValueJSON
			if err := json.Unmarshal(b, &ej); err == nil && ej.Entity != nil {
				*v = EntityUID{
					Type: EntityType(ej.Entity.Type),
					ID:   String(ej.Entity.ID),
				}
				return nil
			}
			rec := Record{}
			err := json.Unmarshal(b, &rec)
			*v = rec
			return err
		}
	}

	var res interface{}
	dec := json.NewDecoder(bytes.NewBuffer(b))
	dec.UseNumber()
	if err := dec.Decode(&res); err != nil {
		return fmt.Errorf("%w: %w", errJSONDecode, err)
	}
	switch vv := res.(type) {
	case string:
		*v = String(vv)
	case bool:
		*v = Boolean(vv)
	case json.Number:
		l, err := vv.Int64()
		if err != nil {
			return fmt.Errorf("%w: %w", errJSONLongOutOfRange, err)
		}
		*v = Long(l)
	default:
		return errJSONUnsupportedType
	}
	return nil
}

func unmarshalExtensionValue[T any](b []byte, extName string, parse func(string) (T, error)) (T, error) {
	var zeroT T
	var arg string
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &arg); err != nil {
			return zeroT, errors.Join(errJSONDecode, err)
		}
	} else {
		var res extValueJSON
		if err := json.Unmarshal(b, &res); err != nil {
			return zeroT, errors.Join(errJSONDecode, err)
		}
		if res.Extn == nil {
			// If we didn't find an Extn, maybe it's just an extn.
			var res2 extn

			if err := json.Unmarshal(b, &res2); err != nil {
				return zeroT, errors.Join(errJSONDecode, err)
			}

			// We've tried Ext.Fn and Fn, so no good.
			if res2.Fn == "" {
				return zeroT, errJSONExtNotFound
			}
			if res2.Fn != extName {
				return zeroT, errJSONExtFnMatch
			}
			arg = res2.Arg
		} else if res.Extn.Fn != extName {
			return zeroT, errJSONExtFnMatch
		} else {
			arg = res.Extn.Arg
		}
	}

	v, err := parse(arg)
	if err != nil {
		return zeroT, err
	}

	return v, nil
}
