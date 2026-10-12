// Package secret stores the TypeSafe API key in the operating system keyring.
//
// The key is passed on stdin to the platform tool. It is not placed on a
// command line, in the project, or in the shell profile.
package secret

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const (
	service = "lintel"
	account = "typesafe"
	label   = "Lintel TypeSafe API key"
	target  = "lintel/typesafe"
)

// ErrNotFound means the keyring has no TypeSafe API key.
var ErrNotFound = errors.New("no TypeSafe API key in the system keyring")

type commandError struct {
	Code    int
	Text    string
	Missing bool
}

func (e *commandError) Error() string {
	if e.Missing {
		return e.Text
	}
	if e.Text != "" {
		return e.Text
	}
	return fmt.Sprintf("exit %d", e.Code)
}

// Runner executes a keyring tool. Tests replace it.
var runCommand = defaultRun

func defaultRun(name string, args []string, stdin string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", &commandError{Missing: true, Text: missingTool(name)}
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		text := strings.TrimSpace(stderr.String())
		return stdout.String(), &commandError{Code: exit.ExitCode(), Text: text}
	}
	return "", err
}

func missingTool(name string) string {
	switch name {
	case "secret-tool":
		return "secret-tool is not installed. Install libsecret, then run lintel login again."
	case "security":
		return "The macOS security command is not available."
	default:
		return name + " is not available."
	}
}

// Save stores the key in the system keyring.
func Save(key string) error {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return errors.New("A TypeSafe API key is required.")
	}
	switch runtime.GOOS {
	case "darwin":
		return saveDarwin(key)
	case "windows":
		return saveWindows(key)
	default:
		return saveLinux(key)
	}
}

// Load returns the key stored by Save.
func Load() (string, error) {
	var (
		out string
		err error
	)
	switch runtime.GOOS {
	case "darwin":
		out, err = runCommand("security", []string{"find-generic-password", "-a", account, "-s", service, "-w"}, "")
	case "windows":
		out, err = powershell("lookup", "")
	default:
		out, err = runCommand("secret-tool", []string{"lookup", "service", service, "account", account}, "")
	}
	if err != nil {
		var cmdErr *commandError
		if errors.As(err, &cmdErr) && !cmdErr.Missing {
			return "", ErrNotFound
		}
		return "", err
	}
	key := strings.TrimSpace(out)
	if key == "" {
		return "", ErrNotFound
	}
	return key, nil
}

// Remove deletes the stored key. A missing key is success.
func Remove() error {
	var err error
	switch runtime.GOOS {
	case "darwin":
		_, err = runCommand("security", []string{"delete-generic-password", "-a", account, "-s", service}, "")
	case "windows":
		_, err = powershell("delete", "")
	default:
		_, err = runCommand("secret-tool", []string{"clear", "service", service, "account", account}, "")
	}
	var cmdErr *commandError
	if errors.As(err, &cmdErr) && !cmdErr.Missing {
		return nil
	}
	return err
}

func saveLinux(key string) error {
	_, err := runCommand("secret-tool", []string{
		"store", "--label=" + label, "service", service, "account", account,
	}, key)
	return err
}

func saveDarwin(key string) error {
	command := "add-generic-password -U -a " + account + " -s " + service + " -j " + quoteSecurity(label) + " -w " + quoteSecurity(key)
	_, err := runCommand("security", []string{"-i"}, command+"\n")
	return err
}

func quoteSecurity(value string) string {
	var buf strings.Builder
	buf.WriteByte('"')
	for _, r := range value {
		if r == '\\' || r == '"' {
			buf.WriteByte('\\')
		}
		buf.WriteRune(r)
	}
	buf.WriteByte('"')
	return buf.String()
}

func saveWindows(key string) error {
	_, err := powershell("store", key)
	return err
}

func powershell(mode, secret string) (string, error) {
	file, err := os.CreateTemp("", "lintel-cred-*.ps1")
	if err != nil {
		return "", err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.WriteString(windowsScript); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return runCommand("powershell", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", name, mode}, secret)
}

const windowsScript = `param([Parameter(Mandatory = $true)][ValidateSet('store','lookup','delete')][string]$Mode)
$ErrorActionPreference = 'Stop'
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class LintelCred {
  [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
  public struct Credential {
    public uint Flags;
    public uint Type;
    public string TargetName;
    public string Comment;
    public System.Runtime.InteropServices.ComTypes.FILETIME LastWritten;
    public uint CredentialBlobSize;
    public IntPtr CredentialBlob;
    public uint Persist;
    public uint AttributeCount;
    public IntPtr Attributes;
    public string TargetAlias;
    public string UserName;
  }
  [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
  public static extern bool CredWrite(ref Credential credential, uint flags);
  [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
  public static extern bool CredRead(string target, uint type, uint reserved, out IntPtr credential);
  [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
  public static extern bool CredDelete(string target, uint type, uint flags);
  [DllImport("advapi32.dll")]
  public static extern void CredFree(IntPtr cred);
}
"@
$target = 'lintel/typesafe'
if ($Mode -eq 'store') {
  $secret = [Console]::In.ReadToEnd().Trim()
  if (-not $secret) { throw 'empty key' }
  $bytes = [Text.Encoding]::Unicode.GetBytes($secret)
  $blob = [Runtime.InteropServices.Marshal]::AllocCoTaskMem($bytes.Length)
  [Runtime.InteropServices.Marshal]::Copy($bytes, 0, $blob, $bytes.Length)
  $cred = New-Object LintelCred+Credential
  $cred.Type = 1
  $cred.TargetName = $target
  $cred.Comment = 'Lintel TypeSafe API key'
  $cred.CredentialBlobSize = [uint32]$bytes.Length
  $cred.CredentialBlob = $blob
  $cred.Persist = 2
  $cred.UserName = 'typesafe'
  $ok = [LintelCred]::CredWrite([ref]$cred, 0)
  [Runtime.InteropServices.Marshal]::FreeCoTaskMem($blob)
  if (-not $ok) { throw ("CredWrite failed: " + [Runtime.InteropServices.Marshal]::GetLastWin32Error()) }
  exit 0
}
if ($Mode -eq 'lookup') {
  $ptr = [IntPtr]::Zero
  $ok = [LintelCred]::CredRead($target, 1, 0, [ref]$ptr)
  if (-not $ok) { exit 1 }
  $cred = [Runtime.InteropServices.Marshal]::PtrToStructure($ptr, [type][LintelCred+Credential])
  $size = [int]$cred.CredentialBlobSize
  $bytes = New-Object byte[] $size
  [Runtime.InteropServices.Marshal]::Copy($cred.CredentialBlob, $bytes, 0, $size)
  [LintelCred]::CredFree($ptr)
  [Console]::Out.Write([Text.Encoding]::Unicode.GetString($bytes).TrimEnd([char]0))
  exit 0
}
[void][LintelCred]::CredDelete($target, 1, 0)
exit 0
`
