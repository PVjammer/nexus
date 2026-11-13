package widget

// Ship marks a widget as shipped.
func Ship(id string) error { return validate(id) }

func validate(id string) error {
	if id == "" {
		return errEmpty
	}
	return nil
}

var errEmpty = error(nil)
