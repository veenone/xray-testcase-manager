package kiwi

import (
	"fmt"
	"strconv"
	"strings"
)

// parseKiwiID parses a neutral entity key (Kiwi's stringified pk — spec
// §3.2: "Key <- id (pk, as string) ... IDStyle=numeric") back into the int
// Kiwi's JSON-RPC filter dicts expect (e.g. {"pk": id}, {"pk__in": [...]}).
func parseKiwiID(key string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(key))
	if err != nil {
		return 0, fmt.Errorf("kiwi: invalid numeric id %q: %w", key, err)
	}
	return id, nil
}
