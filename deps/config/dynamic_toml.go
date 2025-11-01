package config

import (
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

// TransparentMarshal marshals a struct to TOML, treating Dynamic[T] fields transparently.
// Dynamic[T] fields are unwrapped and their inner values are marshaled directly.
func TransparentMarshal(v interface{}) ([]byte, error) {
	// Create a shadow struct with Dynamic fields unwrapped
	shadow := unwrapDynamics(v)
	return toml.Marshal(shadow)
}

// TransparentUnmarshal unmarshals TOML into a struct, treating Dynamic[T] fields transparently.
// Values are decoded into temporary structs then wrapped in Dynamic.
// NOTE: For types like types.FIL with unexported pointer fields, the target must be pre-initialized
// with default values (e.g., types.MustParseFIL("0")) before calling this function.
func TransparentUnmarshal(data []byte, v interface{}) error {
	_, err := TransparentDecode(string(data), v)
	return err
}

// TransparentDecode decodes TOML into a struct, treating Dynamic[T] fields transparently.
// Like toml.Decode, it returns MetaData for checking which fields were set.
// NOTE: For types like types.FIL with unexported pointer fields, the target must be pre-initialized
// with default values (e.g., types.MustParseFIL("0")) before calling this function.
// NOTE: FixTOML should be called BEFORE this function to ensure proper slice lengths and FIL initialization.
func TransparentDecode(data string, v interface{}) (toml.MetaData, error) {
	// Create a shadow struct to decode into
	shadow := createShadowStruct(v)

	// Initialize shadow with values from target (for types like FIL that need non-nil pointers)
	// This copies FIL values that were initialized by FixTOML
	initializeShadowFromTarget(shadow, v)

	// Decode into shadow and get metadata
	// Note: TOML will overwrite slice elements, but our initialized FIL fields will be preserved
	// because TOML calls UnmarshalText on FIL types, which requires non-nil pointers
	md, err := toml.Decode(data, shadow)
	if err != nil {
		return md, err
	}

	// Copy values from shadow to Dynamic fields
	err = wrapDynamics(shadow, v)
	return md, err
}

// initializeShadowFromTarget copies initialized values from target to shadow
// This is needed for types like types.FIL that require non-nil internal pointers
func initializeShadowFromTarget(shadow, target interface{}) {
	shadowVal := reflect.ValueOf(shadow)
	targetVal := reflect.ValueOf(target)

	if shadowVal.Kind() == reflect.Ptr {
		shadowVal = shadowVal.Elem()
	}
	if targetVal.Kind() == reflect.Ptr {
		targetVal = targetVal.Elem()
	}

	// Ensure we have structs to work with
	if shadowVal.Kind() != reflect.Struct || targetVal.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < targetVal.NumField(); i++ {
		targetField := targetVal.Field(i)
		shadowField := shadowVal.Field(i)

		if !shadowField.CanSet() || !targetField.IsValid() {
			continue
		}

		if isDynamicTypeForMarshal(targetField.Type()) {
			// For Dynamic fields, copy the inner initialized value to the unwrapped shadow field
			innerVal := extractDynamicValue(targetField)
			if innerVal.IsValid() && innerVal.CanInterface() {
				// Copy the value (including slices with initialized FIL elements from FixTOML)
				val := innerVal.Interface()
				valReflect := reflect.ValueOf(val)
				if valReflect.Type().AssignableTo(shadowField.Type()) {
					shadowField.Set(valReflect)
				}
			}
		} else if targetField.Kind() == reflect.Struct && hasNestedDynamics(targetField.Type()) {
			// For nested structs with Dynamic fields, recursively initialize
			initializeShadowFromTarget(shadowField.Addr().Interface(), targetField.Addr().Interface())
		} else if targetField.Kind() == reflect.Ptr && !targetField.IsNil() && !shadowField.IsNil() {
			// Handle pointers to structs
			elemType := targetField.Type().Elem()
			if elemType.Kind() == reflect.Struct && hasNestedDynamics(elemType) {
				// Recursively initialize pointer to struct with Dynamic fields
				initializeShadowFromTarget(shadowField.Elem().Addr().Interface(), targetField.Elem().Addr().Interface())
			} else if targetField.CanInterface() && shadowField.Type() == targetField.Type() {
				// Copy regular pointer if types match
				shadowField.Set(reflect.ValueOf(targetField.Interface()))
			}
		} else if targetField.CanInterface() && shadowField.Type() == targetField.Type() {
			// Copy regular fields only if types match exactly
			shadowField.Set(reflect.ValueOf(targetField.Interface()))
		}
	}
}

// unwrapDynamics recursively unwraps Dynamic[T] fields for marshaling
func unwrapDynamics(v interface{}) interface{} {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return v
	}

	// Check if this struct has any Dynamic fields - if not, return as-is
	if !hasNestedDynamics(rv.Type()) {
		return v
	}

	// Create a new struct with same fields but Dynamic unwrapped
	shadowType := createShadowType(rv.Type())
	shadowVal := reflect.New(shadowType).Elem()

	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		shadowField := shadowVal.Field(i)

		if !shadowField.CanSet() {
			continue
		}

		if isDynamicTypeForMarshal(field.Type()) {
			// Extract inner value from Dynamic
			innerVal := extractDynamicValue(field)
			if innerVal.IsValid() {
				shadowField.Set(innerVal)
			}
		} else if field.Kind() == reflect.Struct && hasNestedDynamics(field.Type()) {
			// Only recursively unwrap structs that contain Dynamic fields
			shadowField.Set(reflect.ValueOf(unwrapDynamics(field.Interface())))
		} else if field.Kind() == reflect.Ptr && !field.IsNil() {
			// Handle pointers - check if the pointed-to type contains Dynamic fields
			elemType := field.Type().Elem()
			if elemType.Kind() == reflect.Struct && hasNestedDynamics(elemType) {
				// Recursively unwrap the pointed-to struct
				unwrapped := unwrapDynamics(field.Elem().Interface())
				unwrappedPtr := reflect.New(reflect.TypeOf(unwrapped))
				unwrappedPtr.Elem().Set(reflect.ValueOf(unwrapped))
				shadowField.Set(unwrappedPtr)
			} else if field.IsValid() && field.CanInterface() {
				// Regular pointer - copy as-is
				val := field.Interface()
				shadowField.Set(reflect.ValueOf(val))
			}
		} else if field.IsValid() && field.CanInterface() {
			// Copy all other fields via interface (handles types with unexported fields)
			val := field.Interface()
			shadowField.Set(reflect.ValueOf(val))
		}
	}

	return shadowVal.Interface()
}

// createShadowStruct creates a struct for unmarshaling where Dynamic fields are their inner types
func createShadowStruct(v interface{}) interface{} {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}

	shadowType := createShadowType(rv.Type())
	return reflect.New(shadowType).Interface()
}

// createShadowType creates a type with Dynamic[T] fields replaced by T
func createShadowType(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Ptr {
		return reflect.PointerTo(createShadowType(t.Elem()))
	}

	if t.Kind() != reflect.Struct {
		return t
	}

	var fields []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		newField := field

		if isDynamicTypeForMarshal(field.Type) {
			// Replace Dynamic[T] with T
			innerType := extractDynamicInnerType(field.Type)
			newField.Type = innerType
		} else if field.Type.Kind() == reflect.Struct && hasNestedDynamics(field.Type) {
			// Only recursively modify structs that contain Dynamic fields
			newField.Type = createShadowType(field.Type)
		} else if field.Type.Kind() == reflect.Ptr && field.Type.Elem().Kind() == reflect.Struct {
			if hasNestedDynamics(field.Type.Elem()) {
				newField.Type = reflect.PointerTo(createShadowType(field.Type.Elem()))
			}
		}
		// For all other types (including structs without Dynamic fields), keep original type

		fields = append(fields, newField)
	}

	return reflect.StructOf(fields)
}

// wrapDynamics copies values from shadow struct to Dynamic fields
func wrapDynamics(shadow, target interface{}) error {
	shadowVal := reflect.ValueOf(shadow)
	targetVal := reflect.ValueOf(target)

	if shadowVal.Kind() == reflect.Ptr {
		shadowVal = shadowVal.Elem()
	}
	if targetVal.Kind() == reflect.Ptr {
		targetVal = targetVal.Elem()
	}

	// Ensure we have structs to work with
	if shadowVal.Kind() != reflect.Struct || targetVal.Kind() != reflect.Struct {
		return nil
	}

	for i := 0; i < targetVal.NumField(); i++ {
		targetField := targetVal.Field(i)
		shadowField := shadowVal.Field(i)

		if !targetField.CanSet() {
			continue
		}

		if isDynamicTypeForMarshal(targetField.Type()) {
			// Wrap value in Dynamic using Set method
			setDynamicValue(targetField, shadowField)
		} else if targetField.Kind() == reflect.Struct && hasNestedDynamics(targetField.Type()) {
			// Recursively handle nested structs that have Dynamic fields
			err := wrapDynamics(shadowField.Interface(), targetField.Addr().Interface())
			if err != nil {
				return err
			}
		} else if targetField.Kind() == reflect.Ptr && !targetField.IsNil() && !shadowField.IsNil() {
			// Handle pointers to structs
			elemType := targetField.Type().Elem()
			if elemType.Kind() == reflect.Struct && hasNestedDynamics(elemType) {
				// Recursively handle pointer to struct with Dynamic fields
				err := wrapDynamics(shadowField.Elem().Interface(), targetField.Elem().Addr().Interface())
				if err != nil {
					return err
				}
			} else if shadowField.IsValid() && targetField.CanSet() {
				// Regular pointer - copy as-is if types match
				if shadowField.Type().AssignableTo(targetField.Type()) {
					targetField.Set(shadowField)
				}
			}
		} else {
			// Copy regular fields - don't copy if types don't match (shouldn't happen)
			if shadowField.IsValid() && targetField.CanSet() {
				if shadowField.Type().AssignableTo(targetField.Type()) {
					targetField.Set(shadowField)
				}
			}
		}
	}

	return nil
}

// isDynamicTypeForMarshal checks if a type is Dynamic[T]
// (renamed to avoid conflict with isDynamicType in dynamic.go)
func isDynamicTypeForMarshal(t reflect.Type) bool {
	// Handle pointer to Dynamic
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	name := t.Name()
	return strings.HasPrefix(name, "Dynamic[")
}

// hasNestedDynamics checks if a struct type contains any Dynamic[T] fields
func hasNestedDynamics(t reflect.Type) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}

	for i := 0; i < t.NumField(); i++ {
		fieldType := t.Field(i).Type
		if isDynamicTypeForMarshal(fieldType) {
			return true
		}
		// Check nested structs recursively
		if fieldType.Kind() == reflect.Struct && hasNestedDynamics(fieldType) {
			return true
		}
		if fieldType.Kind() == reflect.Ptr && fieldType.Elem().Kind() == reflect.Struct {
			if hasNestedDynamics(fieldType.Elem()) {
				return true
			}
		}
	}
	return false
}

// extractDynamicValue gets the inner value from a Dynamic[T] using reflection
func extractDynamicValue(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}

	// Call Get() method
	getMethod := v.Addr().MethodByName("Get")
	if !getMethod.IsValid() {
		return reflect.Value{}
	}

	results := getMethod.Call(nil)
	if len(results) == 0 {
		return reflect.Value{}
	}

	return results[0]
}

// extractDynamicInnerType gets the T from Dynamic[T]
func extractDynamicInnerType(t reflect.Type) reflect.Type {
	// Handle pointer to Dynamic
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// For Dynamic[T], we need to look at the value field
	if t.Kind() == reflect.Struct && t.NumField() > 0 {
		// The first field is 'value T'
		return t.Field(0).Type
	}

	return t
}

// setDynamicValue sets a Dynamic[T] field using its Set method
func setDynamicValue(dynamicField, valueField reflect.Value) {
	if dynamicField.Kind() == reflect.Ptr {
		if dynamicField.IsNil() {
			// Create new Dynamic instance
			dynamicField.Set(reflect.New(dynamicField.Type().Elem()))
		}
		dynamicField = dynamicField.Elem()
	}

	// Call Set() method
	setMethod := dynamicField.Addr().MethodByName("Set")
	if !setMethod.IsValid() {
		return
	}

	if valueField.IsValid() {
		setMethod.Call([]reflect.Value{valueField})
	}
}
