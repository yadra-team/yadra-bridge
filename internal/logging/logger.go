package logging

import (
	"os"

	"github.com/rs/zerolog"
)

func New() zerolog.Logger {
	return zerolog.New(os.Stdout).
		With().
		Timestamp().
		Str("service", "proxy").
		Logger().
		Level(zerolog.InfoLevel)
}
