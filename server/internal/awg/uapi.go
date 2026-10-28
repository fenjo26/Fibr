package awg

import (
	"bufio"
	"fmt"
	"net"
	"strings"
)

// UAPISockPath returns the path of the amneziawg-go UAPI control socket for
// an interface. The daemon always places its socket in /var/run/amneziawg
// (hardcoded in its ipc package, adjustable only at build time).
func UAPISockPath(iface string) string {
	return fmt.Sprintf("/var/run/amneziawg/%s.sock", iface)
}

// ApplyParams pushes the obfuscation parameters to the running amneziawg-go
// daemon over its UAPI socket. This is the daemon's only configuration
// channel (next to wgctrl, which speaks the same protocol): a "set" applies
// only the keys it carries, so a later wgctrl peer sync never resets the
// AmneziaWG parameters.
func ApplyParams(iface string, p *Params) error {
	return ApplyParamsSock(UAPISockPath(iface), p)
}

// ApplyParamsSock is ApplyParams with an explicit socket path (tests).
func ApplyParamsSock(sockPath string, p *Params) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("refusing to apply invalid awg params: %w", err)
	}
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return fmt.Errorf("dial awg uapi socket %s: %w", sockPath, err)
	}
	defer conn.Close()

	// Cross-platform UAPI: "set=1\n<key=value lines>\n\n", then the daemon
	// answers with "errno=0\n\n" or "errno=<n>\n\n".
	if _, err := fmt.Fprintf(conn, "set=1\n%s\n\n", p.UAPISet()); err != nil {
		return fmt.Errorf("write awg uapi request: %w", err)
	}
	r := bufio.NewReader(conn)
	var status string
	for {
		line, err := r.ReadString('\n')
		if s := strings.TrimSpace(line); s != "" {
			status = s
		}
		if err != nil {
			return fmt.Errorf("read awg uapi response: %w", err)
		}
		if line == "\n" { // terminating empty line
			break
		}
	}
	if status != "errno=0" {
		return fmt.Errorf("awg daemon rejected params: %s", status)
	}
	return nil
}
