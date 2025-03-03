package main

import (
	"bytes"
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/gen2brain/beeep"
	wapi "github.com/iamacarpet/go-win64api"
	"github.com/iamacarpet/go-win64api/shared"
	"golang.org/x/sys/windows"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

var (
	kernel32DLL   = windows.NewLazyDLL("Kernel32.dll")
	debuggerCheck = kernel32DLL.NewProc("IsDebuggerPresent")
)

// readFile (Windows) uses ioutil's ReadFile function and passes the returned
// byte sequence to decodeString.
func readFile(filename string) (string, error) {
	raw, err := ioutil.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return decodeString(string(raw))
}

// decodeString (Windows) attempts to determine the file encoding type
// (typically, UTF-8, UTF-16, or ANSI) and return the appropriately
// encoded string. (HACK)
func decodeString(fileContent string) (string, error) {
	// If contains ~>40% null bytes, we're gonna assume its Unicode
	raw := []byte(fileContent)
	index := bytes.IndexByte(raw, 0)
	if index >= 0 {
		nullCount := 0
		for _, byteChar := range raw {
			if byteChar == 0 {
				nullCount++
			}
		}
		percentNull := float32(nullCount) / float32(len(raw))
		if percentNull < 0.40 {
			return string(raw), nil
		}
	} else {
		return string(raw), nil
	}

	// Make an tranformer that converts MS-Win default to UTF8
	win16be := unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)

	// Make a transformer that is like win16be, but abides by BOM
	utf16bom := unicode.BOMOverride(win16be.NewDecoder())

	// Make a Reader that uses utf16bom
	unicodeReader := transform.NewReader(bytes.NewReader(raw), utf16bom)

	// Decode and print
	decoded, err := ioutil.ReadAll(unicodeReader)
	return string(decoded), err
}

// checkTrace runs WinAPI function "IsDebuggerPresent" to check for an attached
// debugger.
func checkTrace() {
	result, _, _ := debuggerCheck.Call()
	if int(result) != 0 {
		fail("Reversing is cool, but we would appreciate if you practiced your skills in an environment that was less destructive to other peoples' experiences.")
		os.Exit(1)
	}
}

// sendNotification (Windows) employs the beeep library to send notifications
// to the end user.
func sendNotification(messageString string) {
	err := beeep.Notify("Aeacus SE", messageString, dirPath+"assets/img/logo.png")
	if err != nil {
		fail("Notification error: " + err.Error())
	}
}

// rawCmd returns a exec.Command object with the correct PowerShell flags.
//
// rawCmd uses PowerShell's ScriptBlock feature (along with -NoProfile to
// speed things up, as well as some other flags) to run commands on the host
// system and retrieve the return value.
func rawCmd(commandGiven string) *exec.Cmd {
	cmdInput := "powershell.exe -NonInteractive -NoProfile Invoke-Command -ScriptBlock { " + commandGiven + " }"
	debug("rawCmd input: " + cmdInput)
	return exec.Command("powershell.exe", "-NonInteractive", "-NoProfile", "Invoke-Command", "-ScriptBlock", "{ "+commandGiven+" }")
}

// playAudio plays a .wav file with the given path with PowerShell.
func playAudio(wavPath string) {
	info("Playing audio:", wavPath)
	commandText := "(New-Object Media.SoundPlayer '" + wavPath + "').PlaySync();"
	shellCommand(commandText)
}

// adminCheck (Windows) will attempt to open:
//     \\.\PHYSICALDRIVE0
// and will return true if this succeeds, which means the process is running
// as Administrator.
func adminCheck() bool {
	_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
	return err == nil
}

// sidToLocalUser takes an SID as a string and returns a string containing the
// username of the Local User (NTAccount) that it belongs to.
func sidToLocalUser(sid string) string {
	cmdText := "$objSID = New-Object System.Security.Principal.SecurityIdentifier('" + sid + "'); $objUser = $objSID.Translate([System.Security.Principal.NTAccount]); Write-Host $objUser.Value"
	output, _ := shellCommandOutput(cmdText)
	return strings.TrimSpace(output)
}

// localUserToSid takes a username as a string and returns a string containing
// its SID. This is the opposite of sidToLocalUser.
func localUserToSid(userName string) (string, error) {
	return shellCommandOutput("$objUser = New-Object System.Security.Principal.NTAccount('" + userName + "'); $strSID = $objUser.Translate([System.Security.Principal.SecurityIdentifier]); Write-Host $strSID.Value")
}

// getSecedit returns the string value of the secedit.exe command:
//     secedit.exe /export
// which contains security policy options that can't be found in the registry.
func getSecedit() (string, error) {
	return shellCommandOutput("secedit.exe /export /cfg sec.cfg /log NUL; Get-Content sec.cfg; Remove-Item sec.cfg")
}

// getNetUserInfo returns the string output from the command:
//     net user {username}
// in order to get user properties and details.
func getNetUserInfo(userName string) (string, error) {
	return shellCommandOutput("net user " + userName)
}

// getPrograms returns a list of currently installed Programs and
// their versions.
func getPrograms() ([]string, error) {
	softwareList := []string{}
	sw, err := wapi.InstalledSoftwareList()
	if err != nil {
		fail("Couldn't get programs: " + err.Error())
		return softwareList, err
	}
	for _, s := range sw {
		softwareList = append(softwareList, s.Name()+" - version "+s.DisplayVersion)
	}
	return softwareList, nil
}

// getProgram returns the Software struct of program data from a name. The first
// Program that contains the substring passed as the programName is returned.
func getProgram(programName string) (shared.Software, error) {
	prog := shared.Software{}
	sw, err := wapi.InstalledSoftwareList()
	if err != nil {
		fail("Couldn't get programs: " + err.Error())
	}
	for _, s := range sw {
		if strings.Contains(s.Name(), programName) {
			return s, nil
		}
	}
	return prog, errors.New("program not found")
}

func getLocalUsers() ([]shared.LocalUser, error) {
	ul, err := wapi.ListLocalUsers()
	if err != nil {
		fail("Couldn't get local users: " + err.Error())
	}
	return ul, err
}

func getLocalAdmins() ([]shared.LocalUser, error) {
	ul, err := wapi.ListLocalUsers()
	if err != nil {
		fail("Couldn't get local users: " + err.Error())
	}
	var admins []shared.LocalUser
	for _, user := range ul {
		if user.IsAdmin {
			admins = append(admins, user)
		}
	}
	return admins, err
}

func getLocalUser(userName string) (shared.LocalUser, error) {
	userList, err := getLocalUsers()
	if err != nil {
		return shared.LocalUser{}, err
	}
	for _, user := range userList {
		if user.Username == userName {
			return user, nil
		}
	}
	return shared.LocalUser{}, nil
}

func getLocalServiceStatus(serviceName string) (shared.Service, error) {
	serviceDataList, err := wapi.GetServices()
	var serviceStatusData shared.Service
	if err != nil {
		fail("Couldn't get local service: " + err.Error())
		return serviceStatusData, err
	}
	for _, v := range serviceDataList {
		if v.SCName == serviceName {
			return v, nil
		}
	}
	fail(`Specified service '` + serviceName + `' was not found on the system`)
	return serviceStatusData, err
}
