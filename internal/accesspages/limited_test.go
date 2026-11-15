package accesspages

import (
	"strconv"

	"secretsanta/internal/access/limiter"
)

func limiterError() error { return limiter.Error() }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
