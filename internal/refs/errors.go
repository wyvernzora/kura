package refs

import "fmt"

func invalid(kind, value, detail string) error {
	return fmt.Errorf("invalid %s %q; %s", kind, value, detail)
}
