package virtualmachines

import (
	"bytes"
	"errors"
)

// flexBool decodes a flag that the API reports as a number on some versions and as a
// boolean on others. A JSON boolean keeps its value, a JSON number is false when it
// equals 0 and true otherwise, and a JSON null leaves the zero value. Any other JSON
// value is a decode error.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	value := bytes.TrimSpace(data)
	if len(value) == 0 {
		return errors.New("decode a boolean flag: empty JSON value")
	}

	switch {
	case bytes.Equal(value, []byte("null")):
		return nil
	case bytes.Equal(value, []byte("true")):
		*b = true
		return nil
	case bytes.Equal(value, []byte("false")):
		*b = false
		return nil
	}

	switch value[0] {
	case '"':
		return errors.New("decode a boolean flag: want a JSON boolean or number, got a string")
	case '{':
		return errors.New("decode a boolean flag: want a JSON boolean or number, got an object")
	case '[':
		return errors.New("decode a boolean flag: want a JSON boolean or number, got an array")
	}

	*b = flexBool(!isZeroJSONNumber(value))
	return nil
}

// isZeroJSONNumber reports whether a JSON number equals 0: every digit before the
// exponent is 0. Reading the digits keeps a very small or very large number exact.
func isZeroJSONNumber(number []byte) bool {
	if end := bytes.IndexAny(number, "eE"); end >= 0 {
		number = number[:end]
	}
	for _, digit := range number {
		if digit >= '1' && digit <= '9' {
			return false
		}
	}
	return true
}
