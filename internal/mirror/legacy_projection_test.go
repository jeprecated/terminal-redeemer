package mirror

import "strings"

func fixtureSessionIDs(names ...string) map[string]string {
	ids := map[string]string{}
	catalog := activeCatalog(names...)
	for name, session := range catalog.Sessions {
		ids[name] = session.ExactID
	}
	return ids
}

// Historical argv fixtures only. Production launches never use this path.
func legacyPlanLaunch(window Window, cfg LaunchConfig) (LaunchPlan, error) {
	return legacyProjectionPlan(window, cfg, false)
}
func legacyPlanNew(session string, cfg LaunchConfig) (LaunchPlan, error) {
	return legacyProjectionPlan(Window{ZellijSession: session}, cfg, true)
}
func legacyProjectionPlan(window Window, cfg LaunchConfig, create bool) (LaunchPlan, error) {
	session := SessionName(window)
	argv := []string{"env"}
	for _, name := range zellijEnvironment {
		argv = append(argv, "-u", name)
	}
	if cfg.CorrelationToken != "" {
		argv = append(argv, projectionTokenEnvironment+"="+cfg.CorrelationToken)
	}
	argv = append(argv, "zellij", "attach")
	switch {
	case create:
		argv = append(argv, "--create", session, "options", "--on-force-close", "detach")
	case strings.HasPrefix(session, "-"):
		argv = append(argv, "--", session)
	default:
		argv = append(argv, session, "options", "--on-force-close", "detach")
	}
	command := "exec " + QuoteCommand(argv)
	if window.Terminal != nil && window.Terminal.CWD != "" {
		command = "cd -- " + ShellQuote(window.Terminal.CWD) + " 2>/dev/null || true; " + command
	}
	args, err := buildSSHArgs(cfg.SSHOptions, []string{"-tt"}, cfg.SourceHost, command)
	return LaunchPlan{Command: Command{Name: cfg.LauncherCommand, Args: append([]string{"-e", cfg.SSHCommand}, args...)}}, err
}
