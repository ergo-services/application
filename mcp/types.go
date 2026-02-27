package mcp

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"ergo.services/ergo/net/edf"
)

// typeFieldInfo describes a struct field for the AI
type typeFieldInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Tag  string `json:"tag,omitempty"`
}

// listRegisteredTypes returns all EDF-registered types with short names
func listRegisteredTypes() []string {
	types := edf.RegisteredTypes()
	names := make([]string, 0, len(types))
	for fullName := range types {
		names = append(names, fullName)
	}
	return names
}

// lookupType finds a registered type by full or short name
func lookupType(name string) (reflect.Type, bool) {
	return edf.LookupType(name)
}

// describeType returns field info for a struct type
func describeType(t reflect.Type) []typeFieldInfo {
	if t.Kind() != reflect.Struct {
		return nil
	}

	fields := make([]typeFieldInfo, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // skip unexported
		}
		tag := ""
		if jsonTag := f.Tag.Get("json"); jsonTag != "" {
			tag = jsonTag
		}
		fields = append(fields, typeFieldInfo{
			Name: f.Name,
			Type: f.Type.String(),
			Tag:  tag,
		})
	}
	return fields
}

// constructMessage creates a new value of the given type and fills fields from JSON
func constructMessage(t reflect.Type, data json.RawMessage) (any, error) {
	v := reflect.New(t)

	// Try direct JSON unmarshal first
	if err := json.Unmarshal(data, v.Interface()); err != nil {
		// Fallback: manual field assignment from map
		var m map[string]any
		if merr := json.Unmarshal(data, &m); merr != nil {
			return nil, fmt.Errorf("cannot parse message data: %w", err)
		}
		if ferr := fillStructFromMap(v.Elem(), m); ferr != nil {
			return nil, ferr
		}
	}

	return v.Elem().Interface(), nil
}

func fillStructFromMap(v reflect.Value, m map[string]any) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}

		name := field.Name
		if tag := field.Tag.Get("json"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
		}

		val, ok := m[name]
		if ok == false {
			val, ok = m[field.Name]
		}
		if ok == false {
			continue
		}

		if err := setFieldValue(v.Field(i), val); err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}
	}
	return nil
}

func setFieldValue(field reflect.Value, val any) error {
	if val == nil {
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		s, ok := val.(string)
		if ok == false {
			s = fmt.Sprintf("%v", val)
		}
		field.SetString(s)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch v := val.(type) {
		case float64:
			field.SetInt(int64(v))
		case string:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
			field.SetInt(n)
		default:
			return fmt.Errorf("cannot convert %T to int", val)
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch v := val.(type) {
		case float64:
			field.SetUint(uint64(v))
		case string:
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return err
			}
			field.SetUint(n)
		default:
			return fmt.Errorf("cannot convert %T to uint", val)
		}

	case reflect.Float32, reflect.Float64:
		switch v := val.(type) {
		case float64:
			field.SetFloat(v)
		case string:
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return err
			}
			field.SetFloat(n)
		default:
			return fmt.Errorf("cannot convert %T to float", val)
		}

	case reflect.Bool:
		switch v := val.(type) {
		case bool:
			field.SetBool(v)
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			field.SetBool(b)
		default:
			return fmt.Errorf("cannot convert %T to bool", val)
		}

	default:
		// For complex types, try JSON round-trip
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Errorf("cannot marshal value: %w", err)
		}
		ptr := reflect.New(field.Type())
		if err := json.Unmarshal(b, ptr.Interface()); err != nil {
			return fmt.Errorf("cannot unmarshal into %s: %w", field.Type(), err)
		}
		field.Set(ptr.Elem())
	}

	return nil
}
