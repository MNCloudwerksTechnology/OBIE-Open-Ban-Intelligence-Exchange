package selfcheck

import (
	"context"
	"os"
	"os/user"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestHostEnvIsComplete(t *testing.T) {
	env := reflect.ValueOf(HostEnv("/etc/obie/obie.yaml", "obie", "1.2.3"))
	for i := range env.NumField() {
		if f := env.Field(i); f.Kind() == reflect.Func && f.IsNil() {
			t.Errorf("HostEnv leaves %s unset", env.Type().Field(i).Name)
		}
	}
}

func TestOutput(t *testing.T) {
	out, err := output(context.Background(), "sh", "-c", "echo out; echo first >&2; echo second >&2; exit 3")
	if string(out) != "out\n" || err == nil || !strings.HasSuffix(err.Error(), "exit status 3: first") {
		t.Errorf("output = %q, %v", out, err)
	}
	if _, err := output(context.Background(), "obie-no-such-program"); err == nil {
		t.Error("a missing program ran")
	}
}

func TestUsersAndGroups(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	if got := userName(os.Geteuid()); got != me.Username {
		t.Errorf("userName = %q, want %q", got, me.Username)
	}
	if got := userName(1 << 30); got != strconv.Itoa(1<<30) {
		t.Errorf("userName of no user = %q", got)
	}
	t.Setenv("SUDO_USER", "someone-else")
	if want := me.Username; os.Geteuid() != 0 && operator() != want {
		t.Errorf("operator without root = %q, want %q", operator(), want)
	}
	if member, err := inGroup(me.Username, me.Gid); err != nil || !member {
		t.Errorf("inGroup(primary group) = %v, %v", member, err)
	}
	if member, _ := inGroup(me.Username, strconv.Itoa(1<<30)); member {
		t.Error("member of a group that does not exist")
	}
	if _, err := inGroup("obie-no-such-user", me.Gid); err == nil {
		t.Error("inGroup found a user that does not exist")
	}
}
