package crawler

func (c *Crawler) getHostSem(host string) *hostSem {
	c.hostMu.Lock()
	defer c.hostMu.Unlock()
	sem, ok := c.hostSemaphores[host]
	if !ok {
		sem = &hostSem{ch: make(chan struct{}, 2)}
		c.hostSemaphores[host] = sem
	}
	return sem
}
