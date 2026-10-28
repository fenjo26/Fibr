package dns

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"sync"
)

// iptablesComment marks every rule the anti-leak installs, so operators (and
// Remove) can tell Fibr's rules from hand-written ones.
const iptablesComment = "fibr-dns"

// leakRule is one iptables rule: pre carries the table flags (empty for the
// filter table), spec starts with the action verb ("-A CHAIN …") which Apply
// and Remove rewrite to -C / -D.
type leakRule struct {
	pre  []string
	spec []string
}

// AntiLeak closes the encrypted-DNS escape routes from the tunnel subnet:
// plain DNS (udp/tcp 53) aimed at any other server is DNAT-ed back into this
// resolver, and DNS-over-TLS (tcp 853) is rejected. IPv4 only — the tunnel's
// v6 side is not filtered in this MVP.
//
// All errors are warnings, never fatal: on a host without iptables (macOS
// dev box, misprovisioned container) the resolver keeps working, it just
// cannot force clients onto it.
type AntiLeak struct {
	subnet     *net.IPNet
	resolverIP net.IP
	log        *slog.Logger

	// run executes an iptables command; swappable in tests.
	run func(name string, args ...string) error

	mu      sync.Mutex
	applied bool
}

// NewAntiLeak builds the rule installer for a tunnel subnet. resolverIP is
// where hijacked port-53 traffic is redirected to (the resolver's listen
// address inside the subnet).
func NewAntiLeak(subnet net.IPNet, resolverIP net.IP, log *slog.Logger) *AntiLeak {
	if log == nil {
		log = slog.Default()
	}
	return &AntiLeak{
		subnet:     &subnet,
		resolverIP: resolverIP.To4(),
		log:        log,
		run:        runCmd,
	}
}

// rules returns the iptables specs making up the fence.
func (a *AntiLeak) rules() []leakRule {
	s := a.subnet.String()
	dst := a.resolverIP.String()
	return []leakRule{
		{
			// DNS-over-UDP to anyone else in the tunnel -> our resolver.
			pre: []string{"-t", "nat"},
			spec: []string{"-A", "PREROUTING", "-s", s, "-p", "udp", "--dport", "53",
				"!", "-d", dst, "-j", "DNAT", "--to-destination", dst + ":53"},
		},
		{
			// Same for DNS-over-TCP.
			pre: []string{"-t", "nat"},
			spec: []string{"-A", "PREROUTING", "-s", s, "-p", "tcp", "--dport", "53",
				"!", "-d", dst, "-j", "DNAT", "--to-destination", dst + ":53"},
		},
		{
			// DNS-over-TLS: no local endpoint to redirect to -> reject.
			pre:  nil,
			spec: []string{"-A", "FORWARD", "-s", s, "-p", "tcp", "--dport", "853", "-j", "REJECT"},
		},
	}
}

// argv builds the full iptables argument list for a rule with verb (-A/-C/-D).
func (r leakRule) argv(verb string) []string {
	spec := make([]string, len(r.spec))
	copy(spec, r.spec)
	spec[0] = verb
	args := append([]string{}, r.pre...)
	args = append(args, spec...)
	return append(args, "-m", "comment", "--comment", iptablesComment)
}

// Apply installs the rules (idempotent: existing rules are left alone).
func (a *AntiLeak) Apply() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.resolverIP == nil {
		return fmt.Errorf("anti-leak: no IPv4 resolver address")
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		a.log.Warn("anti-leak: iptables not found, DNS traffic may bypass the resolver")
		return nil
	}
	for _, r := range a.rules() {
		if err := a.run("iptables", r.argv("-C")...); err == nil {
			continue // already installed
		}
		if err := a.run("iptables", r.argv("-A")...); err != nil {
			a.log.Warn("anti-leak: cannot install rule", "rule", strings.Join(r.spec, " "), "error", err)
			return fmt.Errorf("anti-leak: iptables: %w", err)
		}
	}
	a.applied = true
	a.log.Info("anti-leak rules applied", "subnet", a.subnet.String(), "resolver", a.resolverIP.String())
	return nil
}

// Remove deletes the rules (idempotent: missing rules are fine).
func (a *AntiLeak) Remove() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := exec.LookPath("iptables"); err != nil {
		return
	}
	for _, r := range a.rules() {
		if err := a.run("iptables", r.argv("-C")...); err != nil {
			continue // not installed
		}
		if err := a.run("iptables", r.argv("-D")...); err != nil {
			a.log.Warn("anti-leak: cannot remove rule", "rule", strings.Join(r.spec, " "), "error", err)
		}
	}
	a.applied = false
}

// runCmd runs a command and folds its output into the error.
func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
