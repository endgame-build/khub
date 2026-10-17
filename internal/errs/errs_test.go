package errs

import "testing"

// The message is printed verbatim, so its bytes are pinned here.
func TestPortNotPermittedMessage(t *testing.T) {
	e := PortNotPermitted(80)
	want := "Not permitted to bind port 80; it needs privileges, the system reserves it, " +
		"or another program holds it. Pass --port <n> to serve on another port"
	if e.Code != "port_not_permitted" || e.Error() != want {
		t.Fatalf("PortNotPermitted(80) = %q %q, want port_not_permitted %q", e.Code, e.Error(), want)
	}
}
