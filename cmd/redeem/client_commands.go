package main

func macClientCommand(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if args[0] != "mirror" {
		switch args[0] {
		case "capture", "resume", "prune", "doctor":
			return false
		}
		return true
	}
	if len(args) == 1 || isHelpToken(args[1]) {
		return true
	}
	switch args[1] {
	case "list", "open", "new", "session-supervisor":
		return true
	}
	return false
}
