package host

import (
	"fmt"
	"os"
	"os/exec"
	"unsafe"
)

/*
#include <syslog.h>
#include <os/log.h>
#include <stdlib.h>

void doLog(int facility, uint8_t type, const char *msg) {
	syslog(facility, "%s", msg);
	os_log_t log = os_log_create("io.nextdns.NextDNS", "daemon");
	os_log_with_type(log, type, "%{public}s", msg);
}
*/
import "C"

const kLOG_DAEMON = 0x18
const kLOG_ERR = 0x3
const kLOG_WARNING = 0x4
const kLOG_INFO = 0x6
const kLOG_DEBUG = 0x7

type macosLogger struct{}

func (l macosLogger) log(facility int, osLogType uint8, msg string) {
	cs := C.CString(msg)
	defer C.free(unsafe.Pointer(cs))
	C.doLog(C.int(facility|kLOG_DAEMON), C.uint8_t(osLogType), cs)
}

func (l macosLogger) Debug(v ...interface{}) {
	l.log(kLOG_DEBUG, C.OS_LOG_TYPE_DEBUG, fmt.Sprint(v...))
}

func (l macosLogger) Debugf(format string, a ...interface{}) {
	l.log(kLOG_DEBUG, C.OS_LOG_TYPE_DEBUG, fmt.Sprintf(format, a...))
}

func (l macosLogger) Info(v ...interface{}) {
	l.log(kLOG_INFO, C.OS_LOG_TYPE_INFO, fmt.Sprint(v...))
}

func (l macosLogger) Infof(format string, a ...interface{}) {
	l.log(kLOG_INFO, C.OS_LOG_TYPE_INFO, fmt.Sprintf(format, a...))
}

func (l macosLogger) Warning(v ...interface{}) {
	l.log(kLOG_WARNING, C.OS_LOG_TYPE_DEFAULT, fmt.Sprint(v...))
}

func (l macosLogger) Warningf(format string, a ...interface{}) {
	l.log(kLOG_WARNING, C.OS_LOG_TYPE_DEFAULT, fmt.Sprintf(format, a...))
}

func (l macosLogger) Error(v ...interface{}) {
	l.log(kLOG_ERR, C.OS_LOG_TYPE_ERROR, fmt.Sprint(v...))
}

func (l macosLogger) Errorf(format string, a ...interface{}) {
	l.log(kLOG_ERR, C.OS_LOG_TYPE_ERROR, fmt.Sprintf(format, a...))
}

func newServiceLogger(name string) (Logger, error) {
	return macosLogger{}, nil
}

func ReadLog(process string) ([]byte, error) {
	return exec.Command("log", "show", "--info", "--debug",
		"--predicate", fmt.Sprintf(`process == "%s" OR sender == "%s"`, process, process),
		"--no-pager", "--style", "syslog").Output()
}

func FollowLog(process string) error {
	cmd := exec.Command("log", "stream", "--level", "debug",
		"--predicate", fmt.Sprintf(`process == "%s" OR sender == "%s"`, process, process),
		"--style", "syslog")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
