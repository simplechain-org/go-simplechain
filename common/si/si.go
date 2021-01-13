package si

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/simplechain-org/go-simplechain/log"
	"reflect"
)

var (
	ErrEmptyString   = errors.New("empty hex string")
	ErrMissingPrefix = errors.New("hex string without Si prefix")
)

const badNibble = ^uint64(0)

func UnmarshalFixedText(typname string, input, out []byte) error {
	raw, err := checkText(input, true)
	if err != nil {
		return err
	}
	if len(raw)/2 != len(out) {
		return fmt.Errorf("string has length %d, want %d for %s", len(raw), len(out)*2, typname)
	}
	// Pre-verify syntax before modifying out.
	for _, b := range raw {
		if decodeNibble(b) == badNibble {
			return errors.New("invalid string")
		}
	}
	hex.Decode(out, raw)
	return nil
}

func checkText(input []byte, wantPrefix bool) ([]byte, error) {
	if len(input) == 0 {
		return nil, nil // empty strings are allowed
	}
	if bytesHaveSiPrefix(input) || bytesHave0xPrefix(input) {
		log.Debug("bytesHaveSiPrefix","input",string(input))
		input = input[2:]
	} else if wantPrefix {
		return nil, errors.New("string without Si prefix")
	}
	if len(input)%2 != 0 {
		return nil, errors.New("string of odd length")
	}
	return input, nil
}

func bytesHaveSiPrefix(input []byte) bool {
	return len(input) >= 2 && input[0] == 'S' && (input[1] == 'i' || input[1] == 'I')
}

func decodeNibble(in byte) uint64 {
	switch {
	case in >= '0' && in <= '9':
		return uint64(in - '0')
	case in >= 'A' && in <= 'F':
		return uint64(in - 'A' + 10)
	case in >= 'a' && in <= 'f':
		return uint64(in - 'a' + 10)
	default:
		return badNibble
	}
}

func MarshalText(b []byte) ([]byte, error) {
	result := make([]byte, len(b)*2+2)
	copy(result, `Si`)
	hex.Encode(result[2:], b)
	return result, nil
}

func UnmarshalFixedJSON(typ reflect.Type, input, out []byte) error {
	if !isString(input) {
		return errors.New("none string")
	}
	return UnmarshalFixedText(typ.String(), input[1:len(input)-1], out)
}

func isString(input []byte) bool {
	return len(input) >= 2 && input[0] == '"' && input[len(input)-1] == '"'
}

func bytesHave0xPrefix(input []byte) bool {
	return len(input) >= 2 && input[0] == '0' && (input[1] == 'x' || input[1] == 'X')
}

func Decode(input string) ([]byte, error) {
	if len(input) == 0 {
		return nil, ErrEmptyString
	}
	if !hasSiPrefix(input) {
		return nil, ErrMissingPrefix
	}
	b, err := hex.DecodeString(input[2:])
	if err != nil {
		return nil, err
	}
	return b, err
}

func hasSiPrefix(input string) bool {
	return len(input) >= 2 && input[0] == 'S' && (input[1] == 'i' || input[1] == 'I')
}