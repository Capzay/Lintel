package prompt

import (
	"os"
	"os/exec"
	"runtime"
)

// carried are the variables a window needs to reach the desktop.
var carried = []string{
	"PATH", "HOME", "DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR",
	"DBUS_SESSION_BUS_ADDRESS", "HYPRLAND_INSTANCE_SIGNATURE", "XAUTHORITY",
}

// Launch starts `exe window <id>` for the project at root, detached from the
// caller so a harness shell timeout cannot close the window. On Linux it asks
// the user's systemd manager first: that runs the window outside a harness
// sandbox (Codex runs commands in one) with the desktop variables carried
// across. It returns an error when no window can be shown.
func Launch(exe, root, id string) error {
	if err := Available(); err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("systemd-run"); err == nil {
			argv := []string{"systemd-run", "--user", "--quiet", "--collect", "--working-directory=" + root,
				"--setenv=LINTEL_PROJECT=" + root}
			for _, name := range carried {
				if value, ok := os.LookupEnv(name); ok {
					argv = append(argv, "--setenv="+name+"="+value)
				}
			}
			argv = append(argv, exe, "window", id)
			if exec.Command(argv[0], argv[1:]...).Run() == nil {
				return nil
			}
		}
	}
	cmd := exec.Command(exe, "window", id)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "LINTEL_PROJECT="+root)
	return detach(cmd)
}
