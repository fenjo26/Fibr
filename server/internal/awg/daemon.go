package awg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const (
	// DaemonBin is the amneziawg-go binary name expected in the image.
	DaemonBin = "amneziawg-go"

	daemonSocketDir = "/var/run/amneziawg"
	// wgctrl scans /var/run/wireguard for userspace UAPI sockets (hardcoded,
	// no env override), while amneziawg-go creates its socket in
	// /var/run/amneziawg (also hardcoded). A symlink bridges the two so the
	// standard wgctrl client — already used by internal/wg — controls the
	// daemon without any forks or patches.
	wgctrlSocketDir = "/var/run/wireguard"

	daemonStartTimeout = 15 * time.Second
)

// Daemon is a running amnezia-wg-go userspace daemon child process.
type Daemon struct {
	cmd    *exec.Cmd
	iface  string
	log    *slog.Logger
	exited chan error // closed-ish: receives cmd.Wait() result once
}

// PrepareRuntime creates the socket directories and the compatibility
// symlink so wgctrl can see the daemon's UAPI socket.
func PrepareRuntime() error {
	if err := os.MkdirAll(daemonSocketDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", daemonSocketDir, err)
	}
	fi, err := os.Lstat(wgctrlSocketDir)
	switch {
	case err == nil && fi.Mode()&os.ModeSymlink != 0:
		// already a symlink — assume ours; verify it points at the awg dir
		if target, _ := os.Readlink(wgctrlSocketDir); target != daemonSocketDir {
			return fmt.Errorf("%s is a symlink to %q, expected %q", wgctrlSocketDir, target, daemonSocketDir)
		}
	case err == nil:
		// a real directory (e.g. leftover from kernel-mode wg or wireguard-tools)
		empty, rerr := os.ReadDir(wgctrlSocketDir)
		if rerr != nil {
			return fmt.Errorf("inspect %s: %w", wgctrlSocketDir, rerr)
		}
		if len(empty) > 0 {
			return fmt.Errorf("%s exists and is not empty; refusing to replace it", wgctrlSocketDir)
		}
		if rmerr := os.Remove(wgctrlSocketDir); rmerr != nil {
			return fmt.Errorf("remove empty dir %s: %w", wgctrlSocketDir, rmerr)
		}
		if lerr := os.Symlink(daemonSocketDir, wgctrlSocketDir); lerr != nil {
			return fmt.Errorf("symlink %s -> %s: %w", wgctrlSocketDir, daemonSocketDir, lerr)
		}
	default:
		if os.IsNotExist(err) {
			if lerr := os.Symlink(daemonSocketDir, wgctrlSocketDir); lerr != nil {
				return fmt.Errorf("symlink %s -> %s: %w", wgctrlSocketDir, daemonSocketDir, lerr)
			}
		} else {
			return fmt.Errorf("stat %s: %w", wgctrlSocketDir, err)
		}
	}
	return nil
}

// StartDaemon launches amneziawg-go in foreground mode as a child process.
// fibr is the container's init process, so the daemon stays a direct child
// and dies with the context (SIGTERM first, SIGKILL after WaitDelay).
func StartDaemon(ctx context.Context, iface string, log *slog.Logger) (*Daemon, error) {
	bin, err := exec.LookPath(DaemonBin)
	if err != nil {
		return nil, fmt.Errorf("%s binary not found (engine=awg needs the amneziawg-go daemon in the image): %w", DaemonBin, err)
	}
	if err := PrepareRuntime(); err != nil {
		return nil, err
	}

	// -f keeps the daemon in the foreground; without it the process re-execs
	// itself detached, escaping our process management.
	cmd := exec.CommandContext(ctx, bin, "-f", iface)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second

	d := &Daemon{cmd: cmd, iface: iface, log: log, exited: make(chan error, 1)}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", DaemonBin, err)
	}
	go func() { d.exited <- cmd.Wait() }()
	log.Info("amneziawg-go daemon started", "pid", cmd.Process.Pid, "iface", iface)
	return d, nil
}

// WaitReady blocks until the daemon's TUN interface and UAPI socket exist,
// the daemon exits prematurely, or the timeout passes.
func (d *Daemon) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	sock := UAPISockPath(d.iface)
	for time.Now().Before(deadline) {
		select {
		case err := <-d.exited:
			return fmt.Errorf("amneziawg-go daemon exited during startup: %w", err)
		default:
		}
		if sockReady(sock) && linkExists(d.iface) {
			d.log.Info("amneziawg-go daemon ready", "iface", d.iface, "uapi", sock)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("amneziawg-go daemon not ready after %s (iface %s, socket %s)", timeout, d.iface, sock)
}

// Stop terminates the daemon gracefully. Idempotent; the context cancellation
// (cmd.Cancel) does the same for free during shutdown.
func (d *Daemon) Stop() error {
	if d.cmd.Process == nil {
		return nil
	}
	if err := d.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-d.exited:
	case <-time.After(5 * time.Second):
		_ = d.cmd.Process.Kill()
		<-d.exited
	}
	d.log.Info("amneziawg-go daemon stopped", "iface", d.iface)
	return nil
}

// Exited reports whether the daemon process has terminated.
func (d *Daemon) Exited() bool {
	select {
	case <-d.exited:
		return true
	default:
		return false
	}
}

func sockReady(path string) bool {
	c, err := net.Dial("unix", path)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func linkExists(name string) bool {
	out, err := exec.Command("ip", "link", "show", name).CombinedOutput()
	return err == nil && len(out) > 0
}
