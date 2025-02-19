package deps

import (
	"bytes"
	"testing"

	"github.com/BurntSushi/toml"
)

type ExampleConfig struct {
	Subsystems struct {
		EnableWindowPost   bool
		WindowPostMaxTasks int
	}
	Fees struct {
		DefaultMaxFee string
	}
}

// An original TOML configuration that has both recognized and unknown fields.
const originalTOML = `
[Subsystems]
  EnableWindowPost = true
  WindowPostMaxTasks = 5

[Fees]
  DefaultMaxFee = "0.07 FIL"

[UnknownSection]
  SomeUnknownKey = "whatever"
  AnotherField   = 123

[AnotherUnknownSection.Nested]
  NestedValue = "I am nested"
`

func TestExtractAndMergeUnknownFields(t *testing.T) {
	//----------------------------------------------------------------------
	// Step 1: Decode original TOML into recognized struct & collect MetaData
	//----------------------------------------------------------------------
	var recognized ExampleConfig
	meta, err := toml.Decode(originalTOML, &recognized)
	if err != nil {
		t.Fatalf("failed to decode recognized fields: %v", err)
	}

	keys := removeUnknownEntries(meta.Keys(), meta.Undecoded())

	//----------------------------------------------------------------------
	// Step 2: Extract the unknown fields using extractUnknownFields
	//----------------------------------------------------------------------
	unknownFields := extractUnknownFields(keys, originalTOML)
	if len(unknownFields) == 0 {
		t.Errorf("expected unknown fields, got none")
	}

	//----------------------------------------------------------------------
	// Step 3: Update recognized fields in the struct
	//----------------------------------------------------------------------
	recognized.Subsystems.EnableWindowPost = false // flip the boolean
	recognized.Subsystems.WindowPostMaxTasks = 10  // change from 5 to 10
	recognized.Fees.DefaultMaxFee = "0.08 FIL"     // update the fee

	//----------------------------------------------------------------------
	// Step 4: Re-encode recognized fields back to TOML
	//----------------------------------------------------------------------
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(recognized); err != nil {
		t.Fatalf("failed to marshal updated recognized config: %v", err)
	}
	updatedConfig := buf.String()

	//----------------------------------------------------------------------
	// Step 5: Merge unknown fields back
	//----------------------------------------------------------------------
	finalConfig, err := mergeUnknownFields(updatedConfig, unknownFields)
	if err != nil {
		t.Fatalf("failed to merge unknown fields: %v", err)
	}

	//----------------------------------------------------------------------
	// Assertions: Check recognized fields have changed & unknown remain
	//----------------------------------------------------------------------
	// 5a. Parse final config into a map to check contents
	var finalMap map[string]interface{}
	if err := toml.Unmarshal([]byte(finalConfig), &finalMap); err != nil {
		t.Fatalf("failed to parse final config: %v\nFinal Config:\n%s", err, finalConfig)
	}

	// 5b. Check recognized fields updated
	subsystems, ok := finalMap["Subsystems"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'Subsystems' in final config")
	}

	if enable, _ := subsystems["EnableWindowPost"].(bool); enable {
		t.Errorf("expected Subsystems.EnableWindowPost = false, got true")
	}
	if tasks, _ := subsystems["WindowPostMaxTasks"].(int64); tasks != 10 {
		t.Errorf("expected Subsystems.WindowPostMaxTasks = 10, got %d", tasks)
	}

	fees, ok := finalMap["Fees"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'Fees' in final config")
	}
	if defaultFee, _ := fees["DefaultMaxFee"].(string); defaultFee != "0.08 FIL" {
		t.Errorf("expected Fees.DefaultMaxFee = '0.08 FIL', got '%s'", defaultFee)
	}

	// 5c. Check unknown fields remain
	if _, exists := finalMap["UnknownSection"]; !exists {
		t.Errorf("expected UnknownSection to remain in final config, but not found")
	}
	if anotherUnknown, exists := finalMap["AnotherUnknownSection"]; !exists {
		t.Errorf("expected AnotherUnknownSection to remain in final config, but not found")
	} else {
		// Inside nested
		nested := anotherUnknown.(map[string]interface{})["Nested"].(map[string]interface{})
		if val, ok := nested["NestedValue"].(string); !ok || val != "I am nested" {
			t.Errorf("expected AnotherUnknownSection.Nested.NestedValue = 'I am nested', got '%v'", val)
		}
	}
}
