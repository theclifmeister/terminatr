package sandbox

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const mode = "restricted token"

var (
	advapi32                  = windows.NewLazySystemDLL("advapi32.dll")
	procCreateRestrictedToken = advapi32.NewProc("CreateRestrictedToken")
	ntdll                     = windows.NewLazySystemDLL("ntdll.dll")
	procNtResumeProcess       = ntdll.NewProc("NtResumeProcess")
)

// CreateRestrictedToken's flags.
const disableMaxPrivilege = 0x1

func check() error {
	if err := errors.Join(procCreateRestrictedToken.Find(), procNtResumeProcess.Find()); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

// start starts cmd suspended with a restricted token at low integrity,
// puts it in a Job Object that allows it no other process and kills it
// with the job, and only then lets it run.
//
// The token keeps only SeChangeNotifyPrivilege, makes Administrators
// deny-only, and restricts every access check to what the RESTRICTED,
// Everyone, Users and logon SIDs are granted as well: the user's own
// files (granted to the user, SYSTEM and Administrators) are refused,
// system files stay readable. Low integrity refuses writes to anything
// not labelled low. pol.Read's files get an ACE granting RESTRICTED read.
func start(cmd *exec.Cmd, pol Policy) (*Proc, error) {
	restricted, err := windows.CreateWellKnownSid(windows.WinRestrictedCodeSid)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	for _, f := range pol.Read {
		if err := grantRead(f, restricted); err != nil {
			return nil, fmt.Errorf("%w: granting %s: %v", ErrUnavailable, f, err)
		}
	}
	tok, err := restrictedToken(restricted)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	job, err := newJob()
	if err != nil {
		tok.Close()
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	release := func() {
		windows.CloseHandle(job)
		tok.Close()
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Token = syscall.Token(tok)
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW
	if err := cmd.Start(); err != nil {
		release()
		return nil, err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME,
		false, uint32(cmd.Process.Pid))
	if err == nil {
		defer windows.CloseHandle(h)
		err = windows.AssignProcessToJobObject(job, h)
	}
	if err == nil {
		if r, _, _ := procNtResumeProcess.Call(uintptr(h)); r != 0 {
			err = fmt.Errorf("NtResumeProcess: status %#x", r)
		}
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		release()
		return nil, fmt.Errorf("sandboxing the started process: %w", err)
	}
	return &Proc{Mode: mode, release: release}, nil
}

// restrictedToken derives the child's primary token from this process's.
func restrictedToken(restricted *windows.SID) (windows.Token, error) {
	var self windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_ADJUST_DEFAULT, &self); err != nil {
		return 0, fmt.Errorf("OpenProcessToken: %w", err)
	}
	defer self.Close()
	everyone, err1 := windows.CreateWellKnownSid(windows.WinWorldSid)
	users, err2 := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	admins, err3 := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	system, err4 := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	low, err5 := windows.CreateWellKnownSid(windows.WinLowLabelSid)
	if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
		return 0, err
	}
	sids := []windows.SIDAndAttributes{{Sid: restricted}, {Sid: everyone}, {Sid: users}}
	logon := logonSID(self)
	if logon != nil {
		sids = append(sids, windows.SIDAndAttributes{Sid: logon})
	}
	disable := []windows.SIDAndAttributes{{Sid: admins}}
	var tok windows.Token
	r, _, err := procCreateRestrictedToken.Call(uintptr(self), disableMaxPrivilege,
		uintptr(len(disable)), uintptr(unsafe.Pointer(&disable[0])), 0, 0,
		uintptr(len(sids)), uintptr(unsafe.Pointer(&sids[0])), uintptr(unsafe.Pointer(&tok)))
	if r == 0 {
		return 0, fmt.Errorf("CreateRestrictedToken: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			tok.Close()
		}
	}()
	label := windows.Tokenmandatorylabel{Label: windows.SIDAndAttributes{Sid: low, Attributes: windows.SE_GROUP_INTEGRITY}}
	if err := windows.SetTokenInformation(tok, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&label)),
		label.Size()); err != nil {
		return 0, fmt.Errorf("low integrity: %w", err)
	}
	// What the process creates (its threads, events) must pass the
	// restricted check too, or it can't open its own objects.
	user, err := self.GetTokenUser()
	if err != nil {
		return 0, err
	}
	trustees := []*windows.SID{user.User.Sid, system, restricted}
	if logon != nil {
		trustees = append(trustees, logon)
	}
	var entries []windows.EXPLICIT_ACCESS
	for _, s := range trustees {
		entries = append(entries, windows.EXPLICIT_ACCESS{AccessPermissions: windows.GENERIC_ALL,
			AccessMode: windows.GRANT_ACCESS, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID,
				TrusteeValue: windows.TrusteeValueFromSID(s)}})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return 0, fmt.Errorf("default DACL: %w", err)
	}
	dacl := struct{ DefaultDacl *windows.ACL }{acl}
	if err := windows.SetTokenInformation(tok, windows.TokenDefaultDacl, (*byte)(unsafe.Pointer(&dacl)),
		uint32(unsafe.Sizeof(dacl))); err != nil {
		return 0, fmt.Errorf("default DACL: %w", err)
	}
	ok = true
	return tok, nil
}

// logonSID is the logon session's SID in tok's groups, or nil.
func logonSID(tok windows.Token) *windows.SID {
	groups, err := tok.GetTokenGroups()
	if err != nil {
		return nil
	}
	for _, g := range groups.AllGroups() {
		if g.Attributes&windows.SE_GROUP_LOGON_ID == windows.SE_GROUP_LOGON_ID {
			s, err := g.Sid.Copy()
			if err == nil {
				return s
			}
		}
	}
	return nil
}

// grantRead adds an ACE granting sid read access to path, keeping its
// other entries.
func grantRead(path string, sid *windows.SID) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	old, _, err := sd.DACL()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{AccessPermissions: windows.GENERIC_READ,
		AccessMode: windows.GRANT_ACCESS, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID,
			TrusteeValue: windows.TrusteeValueFromSID(sid)}}}, old)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// newJob is a Job Object that holds one process (no children), kills it
// when the job's last handle closes (with tm), and denies it the desktop,
// clipboard and system settings.
func newJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("CreateJobObject: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
		windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_DIE_ON_UNHANDLED_EXCEPTION
	info.BasicLimitInformation.ActiveProcessLimit = 1
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err == nil {
		ui := windows.JOBOBJECT_BASIC_UI_RESTRICTIONS{UIRestrictionsClass: windows.JOB_OBJECT_UILIMIT_DESKTOP |
			windows.JOB_OBJECT_UILIMIT_DISPLAYSETTINGS | windows.JOB_OBJECT_UILIMIT_EXITWINDOWS |
			windows.JOB_OBJECT_UILIMIT_GLOBALATOMS | windows.JOB_OBJECT_UILIMIT_HANDLES |
			windows.JOB_OBJECT_UILIMIT_READCLIPBOARD | windows.JOB_OBJECT_UILIMIT_WRITECLIPBOARD |
			windows.JOB_OBJECT_UILIMIT_SYSTEMPARAMETERS}
		_, err = windows.SetInformationJobObject(job, windows.JobObjectBasicUIRestrictions,
			uintptr(unsafe.Pointer(&ui)), uint32(unsafe.Sizeof(ui)))
	}
	if err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("job limits: %w", err)
	}
	return job, nil
}

func lockdown() error { return nil }
