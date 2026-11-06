package virtualmachines

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFlexBoolDecodesEitherForm(t *testing.T) {
	cases := []struct {
		raw       string
		want      bool
		wantError string
	}{
		{raw: `true`, want: true},
		{raw: `false`, want: false},
		{raw: `1`, want: true},
		{raw: `0`, want: false},
		{raw: `2`, want: true},
		{raw: `1.0`, want: true},
		{raw: `null`, want: false},
		{raw: `"1"`, wantError: "got a string"},
		{raw: `{}`, wantError: "got an object"},
		{raw: `[]`, wantError: "got an array"},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			var decoded struct {
				Flag flexBool `json:"flag"`
			}
			err := json.Unmarshal([]byte(`{"flag": `+tc.raw+`}`), &decoded)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("decode %s: want an error naming %q, got %v", tc.raw, tc.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("decode %s: %v", tc.raw, err)
			}
			if bool(decoded.Flag) != tc.want {
				t.Errorf("decode %s: got %v, want %v", tc.raw, bool(decoded.Flag), tc.want)
			}
		})
	}
}
