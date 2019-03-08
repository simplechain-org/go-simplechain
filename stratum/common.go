package stratum

import "errors"

type Auth interface {
	Auth(string, string) bool
}

func handleRecoverError(e interface{}) error {
	switch e.(type) {
	case error:
		{
			return e.(error)
		}
	case string:
		{
			return errors.New(e.(string))
		}
	}
	return ErrUnknown
}
