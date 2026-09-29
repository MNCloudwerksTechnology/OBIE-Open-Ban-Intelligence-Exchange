//go:build tutorial && linux

package tutorial

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// quiet is how long a program must print nothing before a prompt it
	// printed counts as waiting for an answer.
	quiet = 300 * time.Millisecond
	// poll is how often the terminal is looked at.
	poll = 50 * time.Millisecond
)

// promptEnd ends a prompt of the setup assistant that waits for an answer:
// a suggestion in brackets, or the end of the list of peers.
var promptEnd = regexp.MustCompile(`(\]|\(empty: done\)): $`)

// An answer is what the reader types after a prompt.
type answer struct {
	prompt string
	text   string
}

// runInTerminal runs args with a terminal as input and output, as a reader
// types into it: after each prompt it types the next answer and Enter,
// once the prompt is the one the answer follows. It returns everything the
// terminal showed, the typed answers included.
func runInTerminal(ctx context.Context, args []string, answers []answer) (string, error) {
	master, slave, err := openTerminal()
	if err != nil {
		return "", err
	}
	defer func() { _ = master.Close() }()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) // #nosec G204 -- the check's own docker command.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = slave.Close()
		return "", err
	}
	_ = slave.Close()

	var mu sync.Mutex
	var shown bytes.Buffer
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			shown.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return // EIO once the program has ended
			}
		}
	}()
	screen := func() string {
		mu.Lock()
		defer mu.Unlock()
		return shown.String()
	}

	next, answered, seen := 0, -1, -1
	changed := time.Now()
	var typeErr error
loop:
	for {
		select {
		case <-ended:
			break loop
		case <-time.After(poll):
		}
		s := screen()
		if len(s) != seen {
			seen, changed = len(s), time.Now()
			continue
		}
		line := s[strings.LastIndex(s, "\n")+1:]
		if time.Since(changed) < quiet || answered == len(s) || !promptEnd.MatchString(line) {
			continue
		}
		if next == len(answers) || !lineMatches(normalize(answers[next].prompt)[0], normalize(line)[0]) {
			typeErr = fmt.Errorf("the program asks %q, and the page has no answer to it next", strings.TrimSpace(line))
			_ = cmd.Process.Kill()
			break
		}
		if _, err := master.Write([]byte(answers[next].text + "\r")); err != nil {
			typeErr = err
			break
		}
		next, answered = next+1, len(s)
	}
	waitErr := cmd.Wait()
	<-ended
	out := screen()
	switch {
	case typeErr != nil:
		return out, typeErr
	case waitErr != nil:
		return out, waitErr
	case next < len(answers):
		return out, fmt.Errorf("the program asked %d questions, the page answers %d", next, len(answers))
	}
	return out, nil
}

// openTerminal opens a pseudo-terminal: the master side the check reads
// and types into, the slave side the program runs on.
func openTerminal() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*os.File, *os.File, error) {
		_ = master.Close()
		return nil, nil, err
	}
	fd := int(master.Fd()) // #nosec G115 -- a file descriptor fits an int.
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		return fail(fmt.Errorf("unlock the terminal: %w", err))
	}
	n, err := unix.IoctlGetUint32(fd, unix.TIOCGPTN)
	if err != nil {
		return fail(fmt.Errorf("number of the terminal: %w", err))
	}
	slave, err = os.OpenFile("/dev/pts/"+strconv.FormatUint(uint64(n), 10), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return fail(err)
	}
	// Wide enough that no line the page shows wraps.
	size := &unix.Winsize{Row: 50, Col: 400}
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, size); err != nil { // #nosec G115 -- a file descriptor fits an int.
		_ = slave.Close()
		return fail(errors.Join(errors.New("size the terminal"), err))
	}
	return master, slave, nil
}
