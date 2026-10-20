package bamboohr

import (
	"encoding/json"
	"testing"
)

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "nil", value: nil, want: ""},
		{name: "string", value: "Ada", want: "Ada"},
		{name: "bool", value: true, want: "true"},
		{name: "int", value: 42, want: "42"},
		// A JSON number decodes to float64; an ID must not print as 1.234568e+06.
		{name: "integral float", value: float64(1234567), want: "1234567"},
		{name: "fractional float", value: 3.5, want: "3.5"},
		{name: "negative integral float", value: float64(-8), want: "-8"},
		{name: "json number", value: json.Number("0064"), want: "0064"},
		{name: "slice", value: []any{"a", "b"}, want: `["a","b"]`},
		{name: "map", value: map[string]any{"k": "v"}, want: `{"k":"v"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatValue(tt.value); got != tt.want {
				t.Errorf("FormatValue(%#v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestFormatValueOnDecodedJSON(t *testing.T) {
	// The values a caller passes come from a decoded response, so check the
	// round trip rather than hand-built Go values.
	var decoded map[string]any
	payload := `{"id":1234567,"balance":24.5,"active":true,"note":null,"tags":["a"]}`
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	want := map[string]string{
		"id":      "1234567",
		"balance": "24.5",
		"active":  "true",
		"note":    "",
		"tags":    `["a"]`,
	}
	for key, expected := range want {
		if got := FormatValue(decoded[key]); got != expected {
			t.Errorf("FormatValue(%s) = %q, want %q", key, got, expected)
		}
	}
}

func TestFlexIntDecoding(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    int64
		wantErr bool
	}{
		{name: "number", payload: `1024`, want: 1024},
		{name: "quoted number", payload: `"1024"`, want: 1024},
		{name: "null", payload: `null`, want: 0},
		{name: "empty string", payload: `""`, want: 0},
		{name: "quoted decimal", payload: `"1024.0"`, want: 1024},
		{name: "not a number", payload: `"big"`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got FlexInt
			err := json.Unmarshal([]byte(tt.payload), &got)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) succeeded, want an error", tt.payload)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s): %v", tt.payload, err)
			}
			if got.Int64() != tt.want {
				t.Errorf("Unmarshal(%s) = %d, want %d", tt.payload, got.Int64(), tt.want)
			}
		})
	}
}

func TestFlexIntMarshalsAsNumber(t *testing.T) {
	out, err := json.Marshal(struct {
		Size FlexInt `json:"size"`
	}{Size: 2048})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got, want := string(out), `{"size":2048}`; got != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}
}

func TestFormatValueBlanksZeroDate(t *testing.T) {
	// BambooHR sends a date that was never set as a zero date, which reads
	// like a real value if it is printed as-is.
	if got := FormatValue("0000-00-00"); got != "" {
		t.Errorf("FormatValue(%q) = %q, want empty", zeroDate, got)
	}
	// A real date must survive.
	if got := FormatValue("2025-05-20"); got != "2025-05-20" {
		t.Errorf("FormatValue of a real date = %q, want it unchanged", got)
	}
}
