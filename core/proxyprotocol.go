package core

// ReadsProxyHeaders reports whether an inbound in the running core reads PROXY
// protocol headers (#260), for proxyheader.RefreshInUse. It waits out an
// inbound being added or replaced, which can be accepting before it is listed.
// The copies under core/protocol/ that can read headers say so through
// ReadsProxyHeaders in their users.go.
func (c *Core) ReadsProxyHeaders() bool {
	box, err := c.running()
	if err != nil {
		return false
	}
	for _, inbound := range box.inbound.settled() {
		if reader, isReader := inbound.(interface{ ReadsProxyHeaders() bool }); isReader && reader.ReadsProxyHeaders() {
			return true
		}
	}
	return false
}
