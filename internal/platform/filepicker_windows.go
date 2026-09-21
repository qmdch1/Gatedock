package platform

import (
	"fmt"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// Layout of OPENFILENAMEW from the Windows SDK (commdlg.h).
type openFileName struct {
	Size                         uint32
	Owner, Instance              uintptr
	Filter, CustomFilter         *uint16
	MaxCustomFilter, FilterIndex uint32
	File                         *uint16
	MaxFile                      uint32
	FileTitle                    *uint16
	MaxFileTitle                 uint32
	InitialDir, Title            *uint16
	Flags                        uint32
	FileOffset, FileExtension    uint16
	DefaultExtension             *uint16
	CustomData, Hook             uintptr
	TemplateName                 *uint16
	Reserved                     uintptr
	ReservedValue, FlagsEx       uint32
}

// PickKeyFile returns only a path; it never opens or copies key contents.
func PickKeyFile() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	buffer := make([]uint16, 32768)
	filter := utf16.Encode([]rune("All files (*.*)\x00*.*\x00\x00"))
	title, _ := syscall.UTF16PtrFromString("SSHDesk - Select private key file")
	dir, _ := syscall.UTF16PtrFromString(SSHFile(""))
	of := openFileName{File: &buffer[0], MaxFile: uint32(len(buffer)), Filter: &filter[0], FilterIndex: 1, Title: title, InitialDir: dir,
		Flags: 0x00080000 | 0x00001000 | 0x00000800 | 0x00000008 | 0x00000004}
	of.Size = uint32(unsafe.Sizeof(of))
	dll := syscall.NewLazyDLL("comdlg32.dll")
	ok, _, _ := dll.NewProc("GetOpenFileNameW").Call(uintptr(unsafe.Pointer(&of)))
	if ok == 0 {
		code, _, _ := dll.NewProc("CommDlgExtendedError").Call()
		if code != 0 {
			return "", fmt.Errorf("파일 선택 창 오류: %d", code)
		}
		return "", nil
	}
	return syscall.UTF16ToString(buffer), nil
}
