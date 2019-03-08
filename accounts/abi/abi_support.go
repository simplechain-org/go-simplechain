package abi

func ToGoType(index int, t Type, output []byte) (interface{}, error) {
	return toGoType(index, t, output)
}
