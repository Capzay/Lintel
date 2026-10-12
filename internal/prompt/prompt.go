// Package prompt shows the Jev decision window, used where the harness cannot
// ask the human itself.
//
// A text box whose default button submits approve can confirm itself. The dialog
// has Approve and Reject buttons, and the focused button is Not yet. Only those
// two buttons count. Closing the window, pressing Enter, or submitting the word
// approve does not.
package prompt

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Ask returns the human choice. ok is false when the person has not chosen.
func Ask(card string, runner func(string) (int, string, error)) (string, string, bool) {
	if runner == nil {
		runner = show
	}
	code, output, err := runner(card)
	if err != nil {
		return "", "", false
	}
	return ParseAnswer(code, output)
}

// ParseAnswer accepts only an explicit Approve or Reject from the dialog.
func ParseAnswer(code int, output string) (string, string, bool) {
	if code != 1 {
		return "", "", false
	}
	text := ""
	if trimmed := strings.TrimSpace(output); trimmed != "" {
		lines := strings.Split(trimmed, "\n")
		text = lines[len(lines)-1]
	}
	switch {
	case text == "Approve":
		return "approve", "", true
	case text == "Reject":
		return "reject", "", true
	case strings.HasPrefix(text, "Reject:"):
		return "reject", strings.TrimSpace(strings.SplitN(text, ":", 2)[1]), true
	default:
		return "", "", false
	}
}

// Available reports why no decision window can be shown, or nil when one can.
func Available() error {
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("zenity"); err != nil {
			return errors.New("zenity is not installed")
		}
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return errors.New("no DISPLAY or WAYLAND_DISPLAY in this environment")
		}
	}
	return nil
}

// show opens the window once. Not yet and closing it return no decision; the
// caller decides when to ask again. A window that dies at once is retried,
// because some compositors drop the first map.
func show(card string) (int, string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		code, output, elapsed, err := showChoice(card)
		if err != nil || elapsed < time.Second {
			continue
		}
		label := lastLine(output)
		if code == 1 && label == "Approve" {
			return 1, "Approve", nil
		}
		if code == 1 && label == "Reject" {
			if note := rejectionNote(); note != "" {
				return 1, "Reject: " + note, nil
			}
			return 1, "Reject", nil
		}
		return 0, label, nil
	}
	return 0, "", errors.New("the decision window closed before it could be shown")
}

func showChoice(card string) (int, string, time.Duration, error) {
	if err := Available(); err != nil {
		return 0, "", 0, err
	}
	file, err := os.CreateTemp("", "lintel-decision-*.txt")
	if err != nil {
		return 0, "", 0, err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.WriteString(card + "\n\nClick Approve or Reject. Not yet closes this window and leaves the decision open.\n"); err != nil {
		file.Close()
		return 0, "", 0, err
	}
	file.Close()
	argv := commandFor(name)
	if argv == nil {
		return 0, "", 0, errors.New("no dialog tool")
	}
	code, output, elapsed, err := run(argv)
	if err != nil {
		return code, output, elapsed, err
	}
	return normalizeChoice(code, output, elapsed)
}

func normalizeChoice(code int, output string, elapsed time.Duration) (int, string, time.Duration, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return code, output, elapsed, nil
	}
	label := lastLine(output)
	if label == "Approve" || label == "Reject" {
		return 1, label, elapsed, nil
	}
	return 0, label, elapsed, nil
}

func lastLine(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	return lines[len(lines)-1]
}

func commandFor(filename string) []string {
	switch runtime.GOOS {
	case "darwin":
		script := `set cardPath to "` + escapeApple(filename) + `"
set cardText to read POSIX file cardPath
set answer to button returned of (display dialog cardText buttons {"Not yet", "Reject", "Approve"} default button "Not yet" with title "Jev decision")
return answer`
		return []string{"osascript", "-e", script}
	case "windows":
		return []string{"powershell", "-NoProfile", "-STA", "-Command", windowsScript(filename)}
	default:
		if _, err := exec.LookPath("zenity"); err != nil {
			return nil
		}
		return []string{
			"zenity", "--text-info", "--filename=" + filename, "--title=Jev decision",
			"--ok-label=Not yet", "--cancel-label=Not yet", "--default-cancel",
			"--extra-button=Approve", "--extra-button=Reject", "--width=720", "--height=560",
		}
	}
}

func windowsScript(filename string) string {
	return `
Add-Type -AssemblyName System.Windows.Forms
$text = Get-Content -Raw -LiteralPath '` + strings.ReplaceAll(filename, "'", "''") + `'
$form = New-Object System.Windows.Forms.Form
$form.Text = 'Jev decision'
$form.Width = 720
$form.Height = 560
$form.StartPosition = 'CenterScreen'
$box = New-Object System.Windows.Forms.TextBox
$box.Multiline = $true
$box.ReadOnly = $true
$box.ScrollBars = 'Vertical'
$box.Dock = 'Fill'
$box.Text = $text
$form.Controls.Add($box)
$panel = New-Object System.Windows.Forms.FlowLayoutPanel
$panel.Dock = 'Bottom'
$panel.Height = 48
$script:chosen = ''
$notYet = New-Object System.Windows.Forms.Button
$notYet.Text = 'Not yet'
$notYet.Width = 120
$notYet.Add_Click({ $script:chosen = 'Not yet'; $form.Close() }.GetNewClosure())
$approve = New-Object System.Windows.Forms.Button
$approve.Text = 'Approve'
$approve.Width = 120
$approve.Add_Click({ $script:chosen = 'Approve'; $form.Close() }.GetNewClosure())
$reject = New-Object System.Windows.Forms.Button
$reject.Text = 'Reject'
$reject.Width = 120
$reject.Add_Click({ $script:chosen = 'Reject'; $form.Close() }.GetNewClosure())
$panel.Controls.Add($notYet)
$panel.Controls.Add($approve)
$panel.Controls.Add($reject)
$form.Controls.Add($panel)
$form.AcceptButton = $null
$form.CancelButton = $notYet
$form.ShowDialog() | Out-Null
Write-Output $script:chosen
`
}

func escapeApple(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

func rejectionNote() string {
	var argv []string
	switch runtime.GOOS {
	case "darwin":
		argv = []string{"osascript", "-e", `display dialog "Note for this rejection. Leave it blank if you have none." default answer "" buttons {"No note", "Save note"} default button "No note" with title "Jev decision"`}
	case "windows":
		argv = []string{"powershell", "-NoProfile", "-STA", "-Command", `
Add-Type -AssemblyName Microsoft.VisualBasic
$note = [Microsoft.VisualBasic.Interaction]::InputBox('Note for this rejection. Leave it blank if you have none.', 'Jev decision', '')
Write-Output $note
`}
	default:
		if _, err := exec.LookPath("zenity"); err != nil {
			return ""
		}
		argv = []string{
			"zenity", "--entry", "--title=Jev decision",
			"--text=Note for this rejection. Leave it blank if you have none.",
			"--ok-label=Save note", "--cancel-label=No note", "--width=720",
		}
	}
	code, output, _, err := run(argv)
	if err != nil || code != 0 {
		return ""
	}
	return strings.TrimSpace(output)
}

// Notify shows a desktop notification. It is best effort.
func Notify(title, body string) {
	first := body
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		first = body[:i]
	}
	if len(first) > 160 {
		first = first[:160]
	}
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e", `display notification "`+escapeApple(first)+`" with title "`+escapeApple(title)+`"`).Run()
	case "linux":
		if _, err := exec.LookPath("notify-send"); err == nil {
			_ = exec.Command("notify-send", "--urgency=critical", "--app-name=Lintel", title, first).Run()
		}
	}
}

func run(argv []string) (int, string, time.Duration, error) {
	started := time.Now()
	cmd := exec.Command(argv[0], argv[1:]...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return 0, "", 0, err
	}
	time.Sleep(400 * time.Millisecond)
	focus()
	err := cmd.Wait()
	elapsed := time.Since(started)
	output := buf.String()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code := exit.ExitCode()
			if code != 0 && code != 1 && code != 5 {
				return code, output, elapsed, err
			}
			if strings.Contains(output, "Failed to") {
				return code, output, elapsed, err
			}
			return code, output, elapsed, nil
		}
		return 0, output, elapsed, err
	}
	return 0, output, elapsed, nil
}

func focus() {
	if _, err := exec.LookPath("hyprctl"); err != nil {
		return
	}
	_ = exec.Command("hyprctl", "dispatch", "focuswindow", "title:Jev decision").Run()
}
